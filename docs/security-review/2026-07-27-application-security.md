# Application security review

**Date:** 2026-07-27  
**Scope:** Go HTTP application, OIDC flow, sessions, browser-facing templates/assets, upload processing, filesystem isolation, quotas, metrics, configuration parsing, tests, and resolved Go dependencies at `897315a`. Deployment hardening, installers, containers, live infrastructure, and release engineering were intentionally excluded except where they define an application control boundary.  
**Result:** 3 findings: 1 Medium and 2 Low. No Critical or High finding was confirmed.

## Executive summary

Dumpbox's small application surface is generally well defended. OIDC state and nonce are random, signed, expiring, and verified; ID tokens are library-verified for the configured client; sessions are HMAC-authenticated; browser output is contextually encoded; state-changing requests have same-origin controls; and uploaded names and paths are constrained before private, atomic filesystem publication. Request, file-size, byte-quota, and concurrency limits are enforced on the streaming path.

The principal newly confirmed issue is that the byte quota does not limit the cumulative number of files. An authenticated user can upload unlimited zero-byte files without consuming quota and exhaust the shared filesystem's inodes. Two lower-severity residual risks remain: slow clients can hold upload slots indefinitely unless the documented reverse-proxy rate control exists, and logout deletes only the browser's copy of a session without invalidating another copy.

| Severity | Finding | Primary assumption |
| --- | --- | --- |
| Medium | Per-user byte quotas do not prevent inode exhaustion | An attacker controls one identity accepted by the configured OIDC client and can make sustained upload requests |
| Low | Slow uploads can hold concurrency slots indefinitely | The reverse proxy does not enforce the documented minimum transfer rate or body timeout |
| Low | Logout does not invalidate a copied session token | An attacker has obtained a valid session cookie before the user logs out |

## Attack-surface map

| Entry point / trust boundary | Data flow and sinks | Controls present |
| --- | --- | --- |
| `GET /login` | Browser request → random state/nonce → signed auth cookie → fixed IdP authorization endpoint | `crypto/rand`; HMAC; 10-minute expiry; `HttpOnly`, `SameSite=Lax`, conditional `Secure` |
| `GET /auth/callback` | Query state/code and IdP response → token endpoint/verifier → claims → signed session cookie | Constant-time state/nonce comparison; one browser auth cookie; `go-oidc` issuer/signature/audience/expiry verification; required `sub` |
| Session cookie on `GET /` and `POST /upload` | Cookie → HMAC verification → identity context → HTML or per-subject filesystem directory | Signature before JSON decoding; 12-hour expiry; contextual HTML escaping; immutable subject controls identity |
| `POST /upload` | Multipart headers, filename, and bytes → temporary file → hard link in `DATA_DIR` | Authentication; origin and custom-header checks; request/file/count/concurrency limits; byte quota; filename and directory sanitization; `0700`/`0600`; atomic no-overwrite link |
| `POST /logout` | Browser request → session-cookie deletion | POST; same-origin validation; `SameSite=Lax`; no server-side revocation |
| Environment configuration | Operator strings/secrets → listeners, OIDC client, cookie behavior, filesystem, limits | URL shape/scheme checks; minimum 256-bit session key; positive limit parsing; upload-limit invariant |
| Metrics listener | Network request → Prometheus process/runtime and pseudonymous per-user counters | Separate listener; subject represented by a 96-bit SHA-256 prefix rather than the raw claim |
| OIDC provider | Configured remote discovery/token/JWKS responses → authentication decision | HTTPS required except explicit loopback development mode; fixed configured issuer/client; mature libraries |

## Findings

### [MEDIUM] Per-user byte quotas do not prevent inode exhaustion
**Location:** `internal/dumpbox/server.go:290-339,370-445`; `internal/dumpbox/limits.go:29-71,94-119`  
**Class:** CWE-400 (Uncontrolled Resource Consumption); OWASP API Security Top 10 2023 API4; OWASP ASVS 5.0 V5.2.4  
**Path:** An authenticated caller repeatedly submits up to 100 uniquely named, zero-byte `file` parts per request. `storePart` creates and publishes one regular file for each part. `storageQuota.reserve` receives `bytes == 0`, so `MAX_BYTES_PER_USER` never increases, while `MAX_FILES_PER_REQUEST` limits only one request. Unique names avoid the 10,000-candidate duplicate-name ceiling. There is no cumulative file-count or inode budget per subject.  
**Impact:** One accepted identity can exhaust the shared filesystem's inode table while remaining at zero measured storage usage. Once inodes are exhausted, uploads for every identity fail and other processes sharing the filesystem may also be unable to create files. Medium assumes a sustained authenticated caller; the request volume raises attacker cost, but 100 files can be created per request and the byte quota provides no eventual bound.  
**Fix:** Add a positive `MAX_FILES_PER_USER` setting, rebuild per-subject regular-file counts alongside byte usage at startup, and atomically reserve one file before creating each temporary upload. Release the reservation on every failure path and retain it after publication. Keep a filesystem inode/quota limit as a second enforcing layer. For example:

```go
if !s.storageQuota.reserveFile(user.Subject) {
    return errFileQuotaExceeded
}
published := false
defer func() {
    if !published {
        s.storageQuota.releaseFile(user.Subject)
    }
}()
```

**Confidence:** high; the authenticated multipart-to-`os.CreateTemp`/`os.Link` path is direct, zero-byte writes are accepted, and the cumulative quota accounts only `info.Size()`.

References: [OWASP API4:2023 Unrestricted Resource Consumption](https://owasp.org/API-Security/editions/2023/en/0xa4-unrestricted-resource-consumption/), [OWASP ASVS 5.0 V5 File Handling](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x14-V5-File-Handling.md).

### [LOW] Slow uploads can hold concurrency slots indefinitely
**Location:** `internal/dumpbox/server.go:261-318`; `internal/dumpbox/limits.go:121-153`; `cmd/dumpbox/main.go:38-50`  
**Class:** CWE-400 (Uncontrolled Resource Consumption); OWASP API Security Top 10 2023 API4; OWASP ASVS 5.0 V5.2.1  
**Path:** After authentication, `uploadSlots.acquire` holds a per-user and global slot until the handler returns. The application has no body read deadline or minimum transfer rate; `ReadHeaderTimeout` and `IdleTimeout` do not constrain an active request body. A caller can therefore send a valid multipart body very slowly while remaining below every byte limit. Four streams consume one identity's default slots; eight identities consume all 32 global slots.  
**Impact:** Legitimate uploads receive `429` indefinitely while the slow streams remain connected. Other routes remain available. Low assumes the documented reverse proxy does not enforce a minimum transfer rate or request-body timeout; with that control correctly configured, this path is mitigated.  
**Fix:** Enforce an application read-progress deadline with `http.ResponseController.SetReadDeadline`, renewing it only after meaningful body progress, or require and verify an ingress control that enforces a minimum upload rate. Keep the existing absolute byte and concurrency limits. A progress-aware wrapper should apply a deadline before each read and fail stalled reads without imposing a short total duration on legitimate large uploads:

```go
controller := http.NewResponseController(w)
if err := controller.SetReadDeadline(time.Now().Add(uploadIdleTimeout)); err != nil {
    // Fail closed unless the deployment explicitly supplies the equivalent control.
}
```

**Confidence:** high for the source-to-resource path; medium for practical exploitability because live proxy behavior is outside this review.

Reference: [OWASP API4:2023 Unrestricted Resource Consumption](https://owasp.org/API-Security/editions/2023/en/0xa4-unrestricted-resource-consumption/).

### [LOW] Logout does not invalidate a copied session token
**Location:** `internal/dumpbox/server.go:210-217,244-258,543-557`; `internal/dumpbox/session.go:13-18,30-68`  
**Class:** CWE-613 (Insufficient Session Expiration); OWASP ASVS 5.0 V7.4.1  
**Path:** A session is a self-contained HMAC-signed bearer value valid for 12 hours. `POST /logout` expires only the requesting browser's cookie. The server retains no session identifier or revocation state, so another copy captured before logout continues to pass `signer.verify` and `session.valid` until its original expiry. Rotating `SESSION_SECRET` is the only immediate invalidation mechanism and terminates every user's session.  
**Impact:** An attacker who has already obtained a cookie can continue uploading as the victim after the victim logs out, consuming the victim's quota and placing hostile files in that identity's directory. Low reflects the prerequisite of prior cookie compromise and the 12-hour maximum lifetime; `Secure`, `HttpOnly`, `SameSite`, and HMAC integrity reduce but do not eliminate bearer-token theft.  
**Fix:** Include a cryptographically random session ID in the signed cookie and keep a bounded server-side session registry with expiry. Delete the ID on logout and reject absent or revoked IDs in `requireAuth`. If server-side session state is intentionally rejected, shorten the lifetime substantially and document clearly that logout cannot revoke a copied token, but that remains a partial mitigation. For example:

```go
if !s.sessions.active(user.SessionID, s.now()) {
    s.clearCookie(w, sessionCookie)
    http.Error(w, "Session expired.", http.StatusUnauthorized)
    return
}
```

**Confidence:** high; token validity depends only on signature, subject, and timestamp, and logout changes none of those values server-side.

Reference: [OWASP ASVS 5.0 V7 Session Management](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x16-V7-Session-Management.md).

## Checked and clean

- **OIDC flow:** Random 256-bit state and nonce; signed, expiring auth request; constant-time comparisons; fixed callback; verified issuer, signature, audience, expiry, and nonce; required immutable `sub`. No login-CSRF, token substitution, open redirect, or user-controlled SSRF path was confirmed.
- **Session integrity and cookies:** HMAC-SHA-256 with a minimum 256-bit key; verification precedes JSON decoding; expiry is enforced; cookies are host-only, `HttpOnly`, `SameSite=Lax`, path-scoped, and `Secure` for HTTPS.
- **Authorization and tenant isolation:** The configured OIDC client defines the authorized population by documented design. Signed `sub`, not display claims, controls identity; no read/list/delete route or cross-user object reference exists. The 96-bit hash suffix makes accidental directory collision impractical.
- **CSRF and browser injection:** Upload requires authentication, same origin, and a non-simple custom header; logout is POST and same-origin; `Origin: null` requires `Sec-Fetch-Site: same-origin`. Templates use `html/template`, client filenames use `textContent`, CSP forbids inline code and framing, and no attacker-controlled value reaches a script/style/URL context.
- **Filesystem paths and publication:** Filename base extraction and character mapping prevent traversal; provider usernames are constrained to one path component; private modes are applied; temporary files are removed on errors; hard-link publication atomically refuses overwrite. No remote symlink or path traversal route was found under the single-process ownership model.
- **Upload byte accounting:** `MaxBytesReader` wraps the request before multipart parsing; per-file copying reads at most `max+1`; quota reservations are mutex-protected and released on failures; startup accounting combines directories by subject-hash suffix.
- **Secrets, crypto, and logs:** No tracked secret matched the review scan. Project code does not log tokens, cookies, client secrets, or file contents. Random values use `crypto/rand`; signatures use HMAC-SHA-256 and constant-time verification.
- **Application dependencies:** `go mod verify` passed. Manual advisory research found no public advisory affecting the resolved versions of `go-oidc/v3 v3.20.0`, `x/oauth2 v0.36.0`, `go-jose/v4 v4.1.4`, or `prometheus/client_golang v1.24.1` as of the review date.
- **Metrics:** The application handler explicitly returns 404 for `/metrics`; labels use a fixed-length pseudonymous subject hash instead of raw display claims. Series cardinality still follows the number of authenticated subjects and is therefore part of the IdP client-assignment assumption.

## Informational observations

- Dynamic and authentication responses do not set `Cache-Control: no-store`. The authenticated page contains only a display name and upload controls, and the callback response is a non-cacheable-by-default redirect in typical browsers/proxies, so no concrete sensitive-data disclosure path was confirmed. Explicit `no-store` on login, callback, authenticated HTML, and logout responses would remove dependence on cache defaults.
- `MAX_FILE_BYTES=9223372036854775807` overflows `maxFileBytes+1` in `io.LimitReader`, causing empty files rather than enforcing the configured boundary. This requires an extreme operator configuration and produces availability/data-integrity failure rather than a remotely chosen security bypass.
- Preferred username changes can create more than one directory for a subject, but startup byte accounting combines them by the immutable hash suffix and new writes remain isolated to that same subject.

## Not checked

- Deployment and operations: reverse proxy, TLS termination, DNS, firewall, metrics-network restriction, filesystem quotas/inode limits, backups, malware scanning, monitoring, and incident response.
- Identity-provider tenant policy, user registration, client assignment, federation, claim administration, account disablement, and front/back-channel logout support.
- Installers, Proxmox automation, container hardening, live images, release signatures/provenance, and private repository settings; these were covered by earlier whole-project reviews and are outside this application-focused scope.
- Runtime penetration testing against a deployed instance or a real identity provider.
- GitHub code-scanning and secret-scanning alert contents; both API queries returned `403 Resource not accessible by integration`.
- Automated Go vulnerability results: `govulncheck` could not resolve `vuln.go.dev`, so public advisory research and module verification were used instead.

## Needs a human

- Confirm the IdP restricts assignment to the Dumpbox client to the intended uploaders. The application deliberately treats every identity authenticated for that client as authorized.
- Confirm whether 12-hour non-revocable sessions satisfy the incident-response model and whether IdP logout/account disablement is expected to terminate Dumpbox access immediately.
- Confirm the filesystem enforces both byte and inode limits independently of the in-process quota.
- Confirm the reverse proxy actually applies the documented minimum transfer rate and request-body timeout; otherwise the slow-upload finding is unmitigated.
- Define how operators safely inspect arbitrary uploaded content. Dumpbox stores but does not execute, render, unpack, or scan it.

## Validation record

Manual source-to-sink tracing preceded scanner checks. On 2026-07-27:

- `go mod verify` passed.
- A tracked-file pattern scan found no common private-key, GitHub-token, or AWS-access-key signature outside historical reports.
- Public advisory research found no known affected version among the direct security-relevant Go dependencies.
- `govulncheck ./...` was attempted but could not reach `vuln.go.dev` because DNS resolution failed.
- GitHub code-scanning and secret-scanning alert queries were attempted and both returned `403 Resource not accessible by integration`.
