# Follow-up whole-project security review

**Date:** 2026-07-24
**Scope:** All tracked content at `e5c7286`: Go application source and tests, embedded assets and templates, `Dockerfile`/`compose.yaml`, `scripts/install.sh`, `scripts/proxmox-lxc.sh`, `.github/workflows/release.yml`, module metadata, README, and the tracked `dumpbox` executable.
**Result:** 7 findings: 3 Medium, 4 Low. No Critical or High finding was confirmed. Three of the four findings from `2026-07-24-whole-project.md` are remediated; one is unremediated and is restated here.

## Executive summary

The remediation commit closed the two application-level issues from the previous review well: `MAX_REQUEST_BYTES`/`MAX_FILE_BYTES`/`MAX_FILES_PER_REQUEST` are now enforced on the streaming path, concurrency is capped per subject and globally, `OIDC_ISSUER_URL` must be HTTPS (plaintext only for loopback behind an explicit switch), and HSTS is emitted for HTTPS base URLs. Those fixes are correct as written and covered by tests.

What remains is mostly about *who* may use the service and *how much* they may consume over time, plus supply-chain hygiene. Upload limits are per request; there is still no cumulative per-identity budget, so one account can fill the volume with repeated in-limit requests. More importantly, the application authorizes every identity the configured issuer will authenticate — there is no allowlist mechanism at all, so the security of the deployment is entirely the IdP's registration policy. On the supply-chain side, the root installer and its generated `update` command still execute a mutable default-branch script (only a README warning was added), releases carry no provenance or signature, and a 13 MB unreviewable executable is tracked in the repository.

| Severity | Finding | Primary assumption |
| --- | --- | --- |
| Medium | No cumulative per-identity storage budget | Attacker holds one account the IdP will authenticate; no external filesystem quota is configured |
| Medium | Every identity the issuer authenticates is authorized to upload | The issuer can authenticate identities beyond the intended user set (self-registration, multi-tenant, or federated) |
| Medium | Root install and `update` execute a mutable branch | Repository, maintainer account, or delivery path is compromised at install/update time |
| Low | Compiled `dumpbox` executable tracked in the repository | A user runs the checked-in binary, or a malicious binary change passes review as a `Bin` diff |
| Low | Unicode bidi/format characters survive filename sanitization | A human later browses `/var/lib/dumpbox` in a bidi-rendering file manager or terminal |
| Low | Upload slots have no time bound | Attacker can open slow-body uploads and hold slots |
| Low | Release artifacts have no provenance or signature | Attacker can publish or substitute release assets |

## Remediation status of the previous review

| Previous finding | Status |
| --- | --- |
| Uploads have no resource budget | Partially remediated. Per-request, per-file, per-request-file-count, per-user and global concurrency limits exist (`internal/dumpbox/config.go:14-19`, `internal/dumpbox/limits.go:1-54`, `internal/dumpbox/server.go:244-312`). No cumulative quota and no upload duration or rate bound. See [MEDIUM] below. |
| Plaintext OIDC issuers are accepted | Remediated. `validateIssuer` (`internal/dumpbox/config.go:90-113`) requires absolute HTTPS with no userinfo/query/fragment; `http` only for loopback hosts with `OIDC_ALLOW_INSECURE_ISSUER=true`. |
| Root installation executes a mutable branch | Not remediated. Only a README warning was added (`README.md:76-88`). Restated below. |
| HTTPS deployments do not emit HSTS | Remediated. `internal/dumpbox/server.go:474-476` emits `max-age=31536000` when `BASE_URL` is HTTPS; documented in `README.md:30-33`. |

## Attack-surface map

| Entry point / trust boundary | Data flow and sinks | Controls present at this revision |
| --- | --- | --- |
| `GET /login`, `GET /auth/callback` | Query parameters and IdP responses → token exchange → verifier → signed session cookie | 256-bit state/nonce, HMAC-signed 10-minute auth cookie, constant-time state and nonce comparison, `go-oidc` issuer/audience/signature verification, HTTPS-only issuer |
| Session cookie on `GET /`, `POST /upload`, `POST /logout` | Cookie → HMAC verify → identity in context → per-user directory | HMAC-SHA-256, ≥256-bit key, 12-hour expiry, `HttpOnly`, `SameSite=Lax`, conditional `Secure`, path-scoped |
| `POST /upload` | Multipart filename and bytes → temp file → hard link into `DATA_DIR/user-*` | Same-origin check plus non-simple `X-Dumpbox-Upload` header, `MaxBytesReader`, per-file `LimitReader`, file-count cap, concurrency slots, `0700`/`0600` modes, no-overwrite publication |
| Environment configuration | Operator strings → OIDC client, listener, filesystem | Issuer scheme/shape validation, base URL must have no path, session key length floor, positive-integer limit parsing, per-user ≤ global concurrency invariant |
| Outbound OIDC discovery/token/JWKS | Configured issuer → HTTP client → auth decisions | HTTPS enforced, 15-second startup discovery timeout, no user-controlled outbound URL |
| Installers (`install.sh`, `proxmox-lxc.sh`) | Operator input and GitHub content → root shell, systemd, `pct` | Input validation and `printf %q` quoting, TLS downloads, release checksum verification, dedicated system user, hardened unit, unprivileged LXC |
| Container/Compose | Build context and env → listener and volume | `.dockerignore` excludes `.git`/`.env`/`data`/`dumpbox`, scratch image, UID/GID 65532, named volume |
| Release workflow | Tag → tests → archives → GitHub release | `permissions: contents: read` default with scoped `contents: write`, SHA-pinned actions, tag regex validation, `go test -race`/`go vet` gate, no untrusted `github.event.*` interpolation |
| Templates and embedded SVG | IdP display name and client filenames → DOM | `html/template` escaping, `textContent` for filenames, CSP, `X-Frame-Options`, `nosniff`, referrer and permissions policy, HSTS |

## Findings

### [MEDIUM] No cumulative per-identity storage budget
**Location:** `internal/dumpbox/server.go:244-312`; `internal/dumpbox/limits.go:1-54`; `README.md:56-61`
**Class:** CWE-400 (Uncontrolled Resource Consumption); OWASP API Security Top 10 2023 API4; OWASP ASVS 5.0 V5.2
**Path:** Every limit in `uploadLimits` is scoped to a single request. An authenticated caller sends request after request, each within `MAX_REQUEST_BYTES` (5 GiB by default) and each within the per-user concurrency cap of 4. `storePart` writes every accepted byte to `DATA_DIR`, and nothing tracks bytes or file count already stored for that subject. Files are never expired or deleted by the application.
**Impact:** One account exhausts the data volume. On the documented LXC and systemd installs, `DATA_DIR` is `/var/lib/dumpbox` on the container root filesystem, so a full disk also stops journald writes and can degrade the container's own services, not just uploads. All other users lose the ability to upload. Medium assuming the attacker holds one IdP account and the operator has not configured a filesystem quota; Low where the volume is separate and quota-enforced, which the README recommends but nothing verifies.
**Fix:** Track per-subject usage and reserve before publishing. Maintain a per-subject byte counter (rebuilt at startup by walking each user directory, updated under the same mutex that guards `uploadSlots`), reject with `413`/`507` when a part would exceed `MAX_BYTES_PER_USER`, and delete the temporary file on rejection. Because a process restart or a bug can desynchronize any in-process counter, keep the filesystem quota as the enforcing control and treat the application counter as the friendly error path:

```go
if !s.quota.reserve(user.Subject, written) {
    return "", errQuotaExceeded
}
```

Optionally add a retention policy (age-based deletion) so long-lived deployments do not grow monotonically.
**Confidence:** high; the source-to-disk path is direct and the absence of any cumulative accounting is stated in the README itself.

References: [OWASP API4:2023](https://owasp.org/API-Security/editions/2023/en/0xa4-unrestricted-resource-consumption/), [ASVS 5.0 V5 File Handling](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x14-V5-File-Handling.md).

### [MEDIUM] Every identity the issuer authenticates is authorized to upload
**Location:** `internal/dumpbox/server.go:130-191,227-242`; `internal/dumpbox/config.go:36-88`
**Class:** CWE-862 (Missing Authorization); OWASP Top 10 2021 A01; OWASP ASVS 5.0 V8.1
**Path:** `callback` accepts any ID token that `go-oidc` verifies for the configured client, extracts `sub`, and issues a 12-hour session. `requireAuth` checks only that the session signature is valid and unexpired. There is no group, role, `email_verified`, hosted-domain, or explicit allowlist check anywhere, and no configuration knob to add one. Authentication is therefore the whole of authorization: whoever the issuer will authenticate for this client can create a private folder and write to disk.
**Impact:** If the configured issuer is multi-tenant, federated, or permits self-registration — and nothing in Dumpbox or its documentation constrains that choice — arbitrary internet users become authorized writers, which also makes the storage-exhaustion finding above cheap to exploit and turns the data directory into an anonymous drop for hostile content that an administrator will later handle. Medium under the assumption that the issuer can authenticate identities beyond the intended user set; Informational for a single-tenant IdP whose client-side policy already restricts assignment to specific users or groups.
**Fix:** Add an explicit authorization gate applied after ID-token verification and before the session is issued. Support at least one of: allowlist of `sub` values, allowlist of verified email addresses or email domains (only when the issuer sets `email_verified`), or a required claim value such as a group or role from a configured claim name. Fail closed when the claim is absent. Document that the IdP-side client assignment is a second control, not the only one.
**Confidence:** high for the absence of the control; medium for severity, because impact depends on the operator's issuer, which is deployment context this review cannot see.

Reference: [ASVS 5.0 V8 Authorization](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x17-V8-Authorization.md).

### [MEDIUM] Root install and `update` execute a mutable branch (carried over, unremediated)
**Location:** `scripts/install.sh:99-112`; `scripts/proxmox-lxc.sh:4,196-203`; `README.md:65-88,120-126`
**Class:** CWE-494 (Download of Code Without Integrity Check); OWASP Top 10 2021 A08; NIST SP 800-218 PW.4; OpenSSF Scorecard Pinned-Dependencies
**Path:** Unchanged since the previous review. `install.sh` writes `/usr/local/bin/update`, which runs `curl -fsSL "$INSTALLER_URL" | bash` as root against `refs/heads/main`; the documented Proxmox bootstrap pipes `proxmox-lxc.sh` from `main` into a root shell on the hypervisor, and that script then fetches `install.sh` from `main` inside the container. TLS authenticates GitHub but binds nothing to a reviewed revision. The release checksum check happens after the installer is already executing, so it does not protect the installer.
**Impact:** Compromise of the default branch, a maintainer account, or the delivery path at the moment of install or update yields root command execution — on the Proxmox host in the bootstrap case, which is every hosted workload. The remediation commit added a README warning (`README.md:76-88`), which improves informed consent but does not change the trust model. Severity stays Medium: critical impact, but conditioned on an upstream compromise.
**Fix:** As previously recommended: publish the installer as a versioned release asset with a Sigstore bundle from the release workflow, have the bootstrap and `update` download a specific version and verify it against a certificate identity pinned in the verifier (not against a hash fetched from the same mutable location), and only then execute. As an interim step, generate `update` with the commit SHA of the installer that produced it rather than `main`.
**Confidence:** high for the execution path; medium for likelihood, which depends on upstream compromise.

References: [NIST SP 800-218](https://csrc.nist.gov/pubs/sp/800/218/final), [SLSA build track](https://slsa.dev/spec/v1.0/levels).

### [LOW] Compiled `dumpbox` executable is tracked in the repository
**Location:** `dumpbox` (repository root, 13,278,312 bytes, tracked since at least `57df8b5`, updated in `e5c7286`)
**Class:** CWE-506 (Embedded Malicious Code) as an exposure vector; OpenSSF Scorecard `Binary-Artifacts`; NIST SP 800-218 PW.4/PS.2
**Path:** `go version -m dumpbox` reports a `CGO_ENABLED=1`, dynamically linked, unstripped build of `cmd/dumpbox` at revision `e5c7286`, i.e. a developer-workstation build rather than a release build (releases are `CGO_ENABLED=0 -trimpath -s -w`). It is world-executable in every clone and served by `raw.githubusercontent.com`. Nobody can review it: a future change to it appears in a pull request as `dumpbox | Bin 13262472 -> 13278312 bytes`, so a substituted or trojaned binary is indistinguishable from a rebuild during code review. Running the documented `go build ./cmd/dumpbox` (`README.md:63-69`) overwrites the tracked file and dirties the working tree, which normalizes committing binary churn — this was reproduced during the review.
**Impact:** A user who clones and runs `./dumpbox` — a plausible action for a binary named after the product sitting at the repository root — executes code that no review covered. It also weakens the provenance story: the artifact users can most easily obtain is the one with the least integrity evidence. Low, because the primary distribution paths (release archives with checksums, the Docker build, which excludes the file via `.dockerignore`) do not use it, and exploitation requires a repository compromise or a malicious contributor.
**Fix:** `git rm --cached dumpbox`, add `dumpbox` and `/data` to a `.gitignore`, and keep binaries only in tagged releases. Because it was present in published history, treat any copy already distributed as unverified rather than assuming it matches the source.
**Confidence:** high for the facts; medium for the rating, which depends on whether anyone actually executes the checked-in copy.

Reference: [OpenSSF Scorecard Binary-Artifacts](https://github.com/ossf/scorecard/blob/main/docs/checks.md#binary-artifacts).

### [LOW] Unicode bidi and format characters survive filename sanitization
**Location:** `internal/dumpbox/server.go:389-412`
**Class:** CWE-451 (User Interface Misrepresentation of Critical Information); related to CWE-641; OWASP ASVS 5.0 V5.3
**Path:** `safeFilename` replaces C0/C1 control characters, `DEL`, and the Windows-reserved set `<>:"/\|?*`, trims spaces and dots, and truncates. It does not touch Unicode bidirectional or invisible format characters. Verified against the sanitizer: `"photo\u202Egnp.exe"` and `"\u200Ereport.pdf"` pass through unchanged and are written verbatim into the user's directory.
**Impact:** A file uploaded as `photo<U+202E>gnp.exe` renders as `photoexe.png` in a bidi-aware file manager or terminal. The administrator or downstream consumer who processes `/var/lib/dumpbox` — the documented workflow — can be induced to open an executable believing it is an image. Low: the application never serves, renders, or executes uploads, so the deception only pays off in an external tool, and the operator is already advised to treat uploads as hostile.
**Fix:** Extend the `strings.Map` predicate to replace `unicode.Cf` (format) category runes and the explicit bidi controls, and to reject names that are empty after mapping:

```go
if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Bidi_Control, r) {
    return '_'
}
```

While there, consider replacing a leading `-` so that names cannot be mistaken for option arguments by shell tooling run over the data directory.
**Confidence:** high for reachability, which was reproduced directly against `safeFilename`; medium for impact, which depends on the downstream viewer.

Reference: [Trojan Source (CVE-2021-42574) bidirectional-override class](https://trojansource.codes/).

### [LOW] Upload concurrency slots have no time bound
**Location:** `internal/dumpbox/server.go:250-255`; `internal/dumpbox/limits.go:24-54`; `cmd/dumpbox/main.go:37-42`
**Class:** CWE-400 / CWE-1088; OWASP ASVS 5.0 V5.2.1
**Path:** `uploadSlots.acquire` reserves a slot for the whole handler and releases it only when the handler returns. `http.Server` sets `ReadHeaderTimeout` and `IdleTimeout` but deliberately no `ReadTimeout` or `WriteTimeout`, because legitimate uploads stream for a long time. A client that sends one byte per minute inside a valid multipart part therefore holds a slot, a goroutine, a connection, a file descriptor, and a 256 KiB buffer indefinitely; `MaxBytesReader` bounds total bytes but never elapsed time.
**Impact:** One account can permanently occupy its 4 slots; eight distinct accounts occupy all 32 and make `/upload` return `429` for everyone. Other routes stay available. Low because the per-subject cap forces the attacker to obtain multiple identities for a service-wide effect, and the README already directs operators to configure minimum-transfer-rate limits at the proxy — a control this review cannot verify.
**Fix:** Enforce a minimum transfer rate or an absolute deadline in the handler rather than a server-wide `ReadTimeout`: wrap the request context with a generous per-request deadline derived from `MAX_REQUEST_BYTES` and a floor rate, or use `http.ResponseController.SetReadDeadline` to extend the deadline after each successful chunk so that stalled uploads fail while fast large uploads continue.
**Confidence:** medium; the slot-holding path is certain, but real-world impact depends on how many identities the IdP will issue and on proxy-level rate controls.

### [LOW] Release artifacts have no provenance or signature
**Location:** `.github/workflows/release.yml:37-74`; `scripts/install.sh:74-85`
**Class:** CWE-494; SLSA build track L0→L2 gap; NIST SP 800-218 PS.2; OpenSSF Scorecard `Signed-Releases`
**Path:** The workflow builds archives and publishes `checksums.txt` to the same GitHub release. `install.sh` downloads both from that release and verifies the archive against the neighbouring checksum file. Anyone able to publish or edit release assets — a compromised maintainer token, a workflow with `contents: write` misused later, or a repository takeover — replaces the archive and the checksum file together, and every installer accepts it. The checksum protects against transport corruption, not against a substituted release.
**Impact:** Installed root-run binaries on every host that installs or runs `update` after such a compromise. Low on its own because it requires the same class of compromise as the Medium above and simply fails to add an independent trust anchor.
**Fix:** Add `id-token: write` to the release job and sign the archives and `checksums.txt` with Sigstore keyless signing, publish the bundles as release assets, and generate SLSA provenance (for example via the reusable generator). Have `install.sh` run `cosign verify-blob` against a certificate identity and OIDC issuer pinned in the script before extracting.
**Confidence:** high; the workflow contains no signing or attestation step.

Reference: [OpenSSF Scorecard Signed-Releases](https://github.com/ossf/scorecard/blob/main/docs/checks.md#signed-releases).

## Checked and clean

- **OIDC flow.** State and nonce are 256-bit `crypto/rand` values carried in an HMAC-signed, 10-minute, `HttpOnly` cookie; the cookie is cleared before validation; state and nonce use constant-time comparison; the ID token is verified by `go-oidc` for issuer, signature, audience (`ClientID`), and expiry; `sub` is required and is the only identity used for storage paths. Provider `error` responses are handled after state validation. No login-CSRF, token-substitution, mix-up, or open-redirect path was found — every redirect target is the constant `"/"` derived from a base URL that must not contain a path.
- **Issuer validation.** `validateIssuer` rejects non-HTTPS, userinfo, query, and fragment forms; the `http` escape hatch requires both `OIDC_ALLOW_INSECURE_ISSUER=true` and a loopback host resolved through `netip`, so `http://evil.com#localhost`-style tricks do not pass.
- **Session integrity.** HMAC-SHA-256 over the base64 payload with `hmac.Equal`; the key must decode from base64 to ≥32 bytes; signature is verified before `json.Unmarshal`, so no unauthenticated payload reaches the decoder. Tampered, truncated, and expired values were exercised by the existing tests.
- **CSRF.** `/upload` requires a valid session, a same-origin `Origin` (scheme, host, and normalized port compared against `BASE_URL`, never against the `Host` header), and a non-simple `X-Dumpbox-Upload` header that forces a preflight; `/logout` is POST with the same origin check; cookies are `SameSite=Lax`. The `Origin: null` case correctly falls back to `Sec-Fetch-Site: same-origin`.
- **Header trust.** No handler reads `Host`, `X-Forwarded-*`, `Referer`, or `RemoteAddr`, so a misconfigured proxy cannot influence origin checks, redirects, or cookie scope.
- **Upload limits as implemented.** `MaxBytesReader` is applied before `MultipartReader`; each part is copied through `io.LimitReader(part, max+1)` with an explicit `written > max` check, so the off-by-one boundary is handled; `413` and `429` are distinguished; slot release is deferred and the `active` map entry is deleted at zero, so there is no unbounded map growth or slot leak. Configuration rejects non-positive values and enforces per-user ≤ global.
- **Filesystem handling.** Filenames are reduced with `filepath.Base` after backslash normalization, so no traversal or absolute path survives; directories are `0700` and files `0600`; uploads land in `os.CreateTemp` and are published with `os.Link`, which fails closed on collision and never overwrites; temporary files are removed on every error path via the named-return deferred cleanup; per-user directories combine a sanitized `preferred_username` with 96 bits of the `sub` hash, so two identities cannot collide and no user-supplied value alone determines a directory.
- **Injection and XSS.** All HTML goes through `html/template`; the only interpolated server value is the IdP display name in a text context; client-side filenames use `textContent`; there is no SQL, shell, `os/exec`, reflection, YAML/gob deserialization, XML parser, or user-controlled outbound URL anywhere in the application.
- **Secrets handling.** Secrets come only from the environment; `install.sh` writes `/etc/dumpbox/dumpbox.env` as `0600` root-owned and escapes backslashes and quotes; `proxmox-lxc.sh` uses `printf %q`, `chmod 600` on the temp file, `pct push --perms 0600`, and removes it in the container via `trap`; no secret is logged, echoed, or interpolated into a command line. A pattern scan over tracked files found no key material.
- **Cryptography.** `crypto/rand` for state and nonce, HMAC-SHA-256 for cookies, SHA-256 for directory derivation, constant-time comparison for both the signature and the state; no custom or obsolete primitive.
- **Browser controls.** CSP with `frame-ancestors 'none'`, `base-uri 'none'`, `form-action 'self'`; `nosniff`; `X-Frame-Options: DENY`; `Referrer-Policy: no-referrer`; `Permissions-Policy`; HSTS for HTTPS. Embedded SVGs contain no script, event handler, `foreignObject`, or external reference.
- **CI/CD.** `permissions: contents: read` at workflow level with `contents: write` scoped to the release job; both external actions pinned to full commit SHAs; trigger is `push` on tags only, never `pull_request_target`; `GITHUB_REF_NAME` is used through the environment and a regex gate rather than interpolated into shell; `go test -race` and `go vet` gate the release; no secret is echoed and no artifact carries credentials.
- **Dependencies.** `go mod verify` passed; the module graph is three direct/indirect dependencies. Public advisory research on the review date found no advisory affecting `go-oidc/v3 v3.20.0`, `x/oauth2 v0.36.0` (CVE-2025-22868 is fixed in v0.27.0), or `go-jose/v4 v4.1.4`.
- **Validation run.** `go build ./cmd/dumpbox`, `go vet ./...`, `go test -race ./...`, `bash -n scripts/install.sh scripts/proxmox-lxc.sh`, and `go mod verify` all passed at `e5c7286`.

## Informational observations

These are not findings; no exploit path was demonstrated for any of them.

- **CSP allows `'unsafe-inline'` for `script-src` and `style-src`** (`internal/dumpbox/server.go:469`) because the page ships an inline script and stylesheet. No attacker-controlled value reaches a script or style context today, so this only removes a defence-in-depth layer. Moving the script and CSS to embedded files served from `/assets` and dropping both `'unsafe-inline'` allowances would close it.
- **Sessions are stateless and cannot be revoked.** `POST /logout` clears the cookie but the signed value stays valid for its full 12 hours; a copied cookie survives logout. Rotating `SESSION_SECRET` is the only revocation mechanism, and it logs out everyone.
- **`compose.yaml` publishes `8080` on all interfaces** and sets no `read_only`, `cap_drop`, `security_opt: no-new-privileges`, `mem_limit`, or `pids_limit`. The scratch image and non-root UID limit the value of these, but binding to `127.0.0.1:8080` behind the documented reverse proxy would match the documented architecture.
- **`Dockerfile` uses the mutable tag `golang:1.25-alpine`** rather than a digest, so builds are not reproducible and depend on registry tag integrity.
- **No CI runs on pull requests.** Tests, `vet`, CodeQL, and dependency review execute only on tag pushes, so a defect or an injected change is not caught until release time. Given that changes to this repository arrive through agent-authored pull requests, a `pull_request`-triggered test and code-scanning workflow is the highest-value process addition (NIST SP 800-218 PW.7/PW.8).
- **`storePart` returns an error after a successful publish** if `os.Remove(tempName)` fails (`internal/dumpbox/server.go:365-367`), producing a `500` for a file that was in fact stored, and leaving a stray `.upload-*` entry. Correctness rather than security.
- **Parts whose form name is not `file` are skipped without being counted** (`internal/dumpbox/server.go:285-288`), so a request may contain many such parts; total size is still bounded by `MAX_REQUEST_BYTES`.

## Not checked

- Any live deployment: reverse proxy configuration, TLS, DNS, firewall, storage quotas, backups, monitoring, and incident response.
- The identity provider: registration policy, client assignment, claim contents, `email_verified` semantics, and token lifetimes.
- Runtime or penetration testing against a deployed instance and a real IdP.
- Byte-level provenance of the tracked `dumpbox` executable beyond its `go version -m` build metadata; no disassembly or source-to-binary reproduction was attempted.
- Private repository settings: branch and tag protection, required reviews, Dependabot, secret scanning, code scanning alerts, release environment protection, and maintainer MFA.
- `govulncheck` could not run: the sandbox has no route to `vuln.go.dev`, so dependency conclusions rest on manual advisory research at the review date rather than an automated database query.
- Third-party registry compromise affecting `golang:1.25-alpine`, the Debian LXC template, or `apt` packages installed by the Proxmox script.

## Needs a human

- **Decide who may upload.** This determines whether the missing authorization gate is Informational or the most serious issue in the repository. Confirm whether the configured issuer can authenticate anyone outside the intended user set and whether IdP-side client assignment is actually restricted.
- **Confirm storage quotas exist.** The README delegates per-user disk limits to the filesystem. Verify that a quota or a dedicated volume is in place; if not, the storage finding is effectively unmitigated.
- **Confirm reverse-proxy behaviour**: HTTP-to-HTTPS redirect, its own HSTS policy, request-rate and minimum-transfer-rate limits, request body timeouts, and that it does not forward spoofable headers.
- **Decide whether to keep the checked-in binary.** Removing it from tracking is cheap; deciding what to tell users who may already have run it is not.
- **Confirm Proxmox console access policy.** `scripts/proxmox-lxc.sh:155-167` intentionally configures passwordless root autologin on the container console; acceptable only if every principal with console access is authorized for container root.
- **Confirm downstream handling of uploads.** The service stores arbitrary attacker-supplied bytes and filenames and never scans, unpacks, or serves them; antivirus, content disarm, and safe-viewing are entirely external.
- **Enable and review GitHub code scanning and secret scanning.** Alert queries returned `403 Resource not accessible by integration` during this review, so no claim is made about those alert sets.
- **Decide whether 12-hour non-revocable sessions meet incident-response requirements.**
