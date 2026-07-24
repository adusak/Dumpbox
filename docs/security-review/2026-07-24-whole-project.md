# Whole-project security review

**Date:** 2026-07-24  
**Scope:** All tracked application source, tests, embedded assets, container and Compose configuration, installation scripts, release workflow, Go module metadata, documentation, and the tracked executable in `adusak/Dumpbox`.  
**Result:** 4 findings: 3 Medium, 1 Low. No Critical or High finding was confirmed.

## Executive summary

Dumpbox has a small, understandable attack surface and several sound controls: OIDC state and nonce validation, library-backed issuer/audience/signature verification, HMAC-signed expiring sessions, secure cookie attributes when `BASE_URL` is HTTPS, HTML contextual escaping, cross-origin upload checks, filename and user-directory sanitization, atomic no-overwrite publication, restrictive filesystem modes, a non-root container, a hardened systemd unit, pinned GitHub Actions, and least-privilege workflow permissions.

The principal application risk is availability. Any authenticated identity can stream an unlimited number of unlimited-size uploads for unlimited time. This permits storage exhaustion and, with concurrent slow requests, connection/file-descriptor exhaustion. Authentication integrity also depends on an operator not configuring a plaintext OIDC issuer, but the application does not enforce that condition. The installation path executes a mutable default-branch script as root, making repository/CDN compromise immediately become host or Proxmox-host compromise. Finally, HTTPS deployments rely entirely on an undocumented reverse-proxy HSTS policy.

| Severity | Finding | Primary assumption |
| --- | --- | --- |
| Medium | Uploads have no resource budget | The attacker can obtain one account from the configured IdP. High if public self-registration is allowed; Low if ingress and storage enforce strict external quotas. |
| Medium | Plaintext OIDC issuers are accepted | OIDC traffic can cross an attacker-controlled network. Low if deployment policy independently guarantees an authenticated private network. |
| Medium | Root installation executes a mutable branch | The default branch or its delivery path is compromised when an administrator installs or updates. |
| Low | HTTPS deployments do not emit HSTS | The reverse proxy does not add HSTS. Not applicable if the proxy already enforces the stated policy. |

## Attack-surface map

| Entry point / trust boundary | Data flow and sinks | Existing controls |
| --- | --- | --- |
| `GET /login`, `GET /auth/callback` | Browser parameters and OIDC responses → token exchange/verifier → signed session cookie | Random state and nonce; constant-time comparison; expiry; issuer, audience, signature, and nonce verification through `go-oidc` |
| Session cookies on `/` and `/upload` | Browser cookie → HMAC verification → identity context and per-user directory | HMAC-SHA-256; minimum 256-bit key; expiry; `HttpOnly`; `SameSite=Lax`; conditional `Secure` |
| `POST /upload` | Authenticated multipart filename/content → filesystem directory, temporary file, hard link | Same-origin/custom-header check; path component sanitization; `0700` directories; `0600` files; atomic no-overwrite publication |
| `POST /logout` | Browser request → session-cookie deletion | POST; same-origin check; `SameSite=Lax` |
| Environment/configuration | Administrator-provided URLs, addresses, secrets, and paths → OIDC HTTP client, listener, filesystem | Required secrets; base URL parsing; minimum session-key length |
| OIDC discovery/token/JWKS endpoints | Configured external provider → outbound HTTP client → authentication decisions | Provider and client library validation; startup timeout for discovery |
| Linux/Proxmox installers | Administrator input and GitHub release/default-branch content → root shell, systemd, LXC creation | Input validation/quoting; TLS downloads; release checksums; unprivileged LXC; hardened service |
| Container/runtime configuration | Image/build inputs → network listener and persistent volume | Multi-stage scratch image; numeric non-root user; Go checksum database/`go.sum` |
| Release workflow | Tag and repository content → tests, binaries, GitHub release | SHA-pinned actions; minimal default permissions; scoped `contents: write`; tag validation |
| Embedded SVG/templates | Repository-controlled content and IdP display name → browser DOM | Static SVGs; `html/template`; filename rendering with `textContent`; CSP/frame/referrer/MIME headers |

## Findings

### [MEDIUM] Uploads have no resource budget
**Location:** `internal/dumpbox/server.go:241-304`; `cmd/dumpbox/main.go:38-44`  
**Class:** CWE-400 (Uncontrolled Resource Consumption); OWASP API Security Top 10 2023 API4; OWASP ASVS 5.0 V5.2.1 and V5.2.4  
**Path:** An attacker who can authenticate sends one or many multipart `file` parts. `MultipartReader` and `io.CopyBuffer` continue until the client ends each part, with no request limit, per-file limit, per-user quota, file-count limit, concurrency cap, or body deadline. Every accepted request creates a temporary file and consumes a connection, goroutine, file descriptor, buffer, and persistent disk space. `ReadHeaderTimeout` does not constrain request bodies.  
**Impact:** A single account can fill the shared data volume, preventing all users from uploading and potentially disrupting the host. Concurrent slow uploads can additionally exhaust connections, descriptors, or memory. Severity is Medium assuming an attacker can obtain an IdP account; it is High if internet users can self-register and Low if an authenticated ingress plus filesystem quotas enforce equivalent limits.  
**Fix:** Define enforceable limits in configuration: maximum request/per-file bytes, files per request, aggregate bytes/files per subject, concurrent uploads per subject and globally, and an upload duration or minimum data rate. Apply `http.MaxBytesReader` before `MultipartReader`, copy through an explicit per-file limit, reserve quota atomically before publishing, reject excess with `413`/`429`, remove partial files, and enforce a second storage-level quota so process crashes or races cannot bypass the application check. For example:

```go
r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
limited := &io.LimitedReader{R: part, N: maxFileBytes + 1}
written, err := io.CopyBuffer(temp, limited, buffer)
if err != nil || written > maxFileBytes {
    return "", errFileTooLarge
}
```

Use request-scoped deadlines or ingress minimum-rate controls rather than a single short server `ReadTimeout`, because legitimate large uploads are streamed.  
**Confidence:** high; the authenticated source-to-disk path is direct, and no application, Compose, or documented proxy/storage limit exists.

References: [ASVS V5.2 file upload controls](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x14-V5-File-Handling.md), [OWASP API4:2023 Unrestricted Resource Consumption](https://owasp.org/API-Security/editions/2023/en/0xa4-unrestricted-resource-consumption/).

### [MEDIUM] Plaintext OIDC issuers are accepted
**Location:** `internal/dumpbox/config.go:43-49`; `cmd/dumpbox/main.go:25-27`; `internal/dumpbox/server.go:66-74,141-154`  
**Class:** CWE-319 (Cleartext Transmission of Sensitive Information); OWASP Top 10 2021 A02; OAuth 2.0 Security BCP (RFC 9700)  
**Path:** `OIDC_ISSUER_URL` is only checked for non-emptiness. If an operator supplies `http://...`, discovery, authorization, token exchange, and JWKS retrieval can use plaintext HTTP. An on-path attacker can replace discovery metadata and signing keys while preserving the configured issuer value, then issue an ID token accepted by the verifier for the configured client.  
**Impact:** The attacker can capture the confidential OIDC client secret during token exchange, impersonate arbitrary subjects, obtain valid Dumpbox sessions, write files, and consume shared storage. Severity is Medium because exploitation requires a plaintext deployment configuration and network position; it is Low if the endpoint is independently protected by an authenticated private transport.  
**Fix:** Parse the issuer during configuration loading and require an absolute `https` URL with no userinfo, query, or fragment. Permit HTTP only behind an explicit development-only switch that is rejected outside loopback. Keep exact issuer matching in `go-oidc`:

```go
issuer, err := url.Parse(config.OIDCIssuer)
if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil ||
    issuer.RawQuery != "" || issuer.Fragment != "" {
    return Config{}, errors.New("OIDC_ISSUER_URL must be an absolute HTTPS URL")
}
```

**Confidence:** high; the configuration path reaches OIDC discovery directly, and the accepted scheme is not constrained.

References: [RFC 9700, Protected Resources](https://www.rfc-editor.org/rfc/rfc9700.html), [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html), [OWASP ASVS 5.0 V10 OAuth and OIDC](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x19-V10-OAuth-and-OIDC.md).

### [MEDIUM] Root installation executes a mutable branch
**Location:** `scripts/install.sh:99-112`; `scripts/proxmox-lxc.sh:4,193-203`; `README.md:65-74,102-111`  
**Class:** CWE-494 (Download of Code Without Integrity Check); OWASP Top 10 2021 A08; NIST SP 800-218 PW.4; OpenSSF Scorecard Pinned-Dependencies  
**Path:** The installed `update` command downloads `scripts/install.sh` from the mutable `main` branch and pipes it directly to `bash` as root. The documented Proxmox bootstrap similarly pipes a mutable default-branch script into a root shell on the virtualization host; that script later fetches another mutable installer. TLS authenticates GitHub's endpoint but does not bind content to a reviewed revision. The release archive checksum does not protect the installer because the installer and checksum trust decision occurs first.  
**Impact:** Compromise of the repository default branch, maintainer credentials, or content-delivery path at execution time yields arbitrary root command execution. In the Proxmox bootstrap case, blast radius includes the virtualization host and potentially all hosted workloads. Severity is Medium because a supply-chain compromise is required, despite the critical host impact.  
**Fix:** Publish versioned installer artifacts and detached signatures or provenance from the release workflow. Have bootstrap/update download a specific version, verify it against a key or identity pinned independently of the downloaded content (for example Sigstore keyless verification with repository/workflow identity), and only then execute it. At minimum, generated update commands must use an immutable commit SHA selected from a verified release:

```sh
curl -fL -o "$tmp/install.sh" \
  "https://raw.githubusercontent.com/adusak/Dumpbox/$VERIFIED_COMMIT/scripts/install.sh"
cosign verify-blob --bundle "$tmp/install.sh.bundle" \
  --certificate-identity-regexp 'github.com/adusak/Dumpbox/.github/workflows/release.yml' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "$tmp/install.sh"
bash "$tmp/install.sh"
```

Do not treat a hash downloaded beside the script from the same mutable location as an independent trust anchor.  
**Confidence:** high for the execution path and impact; medium for likelihood because exploitation depends on upstream/repository compromise.

References: [NIST SP 800-218 SSDF](https://csrc.nist.gov/pubs/sp/800/218/final), [OpenSSF Scorecard Pinned-Dependencies](https://github.com/ossf/scorecard/blob/main/docs/checks.md#pinned-dependencies), [SLSA build track](https://slsa.dev/spec/v1.0/levels).

### [LOW] HTTPS deployments do not emit HSTS
**Location:** `internal/dumpbox/server.go:418-426`; `README.md:31-33,92-93`  
**Class:** CWE-319; OWASP ASVS 5.0 V3.4.1  
**Path:** `BASE_URL=https://...` enables `Secure` cookies, but application responses never include `Strict-Transport-Security`. The documented architecture delegates TLS to an unspecified reverse proxy without requiring HSTS. If that proxy also omits HSTS, a first-time browser can be induced to use HTTP, where an on-path attacker can alter the unauthenticated page or redirect the user before the browser establishes an HTTPS-only policy.  
**Impact:** Network attackers can downgrade or modify initial navigation and facilitate credential phishing. Existing `Secure` cookies are not sent over HTTP, limiting direct session theft. Severity is Low and the finding is not applicable when the reverse proxy already emits an adequate HSTS policy.  
**Fix:** Make the proxy requirement explicit and test it, or emit HSTS whenever the configured public base URL is HTTPS. Use at least one year; add `includeSubDomains` only after confirming every subdomain supports HTTPS:

```go
if s.baseURL.Scheme == "https" {
    w.Header().Set("Strict-Transport-Security", "max-age=31536000")
}
```

Also bind the backend to a private interface/network rather than publishing port 8080 broadly when a reverse proxy is used.  
**Confidence:** high that the application omits the header; medium for exploitability because reverse-proxy behavior is outside the repository.

Reference: [OWASP ASVS 5.0 V3.4.1](https://github.com/OWASP/ASVS/blob/master/5.0/en/0x12-V3-Web-Frontend-Security.md#v34-browser-security-mechanism-headers).

## Checked and clean

- **OIDC flow:** Random 256-bit state and nonce, signed/expiring authorization request, state and nonce comparison, ID-token verification, audience binding through client ID, and immutable `sub` use were present. No login-CSRF, token-substitution, or open-redirect path was confirmed.
- **Sessions and CSRF:** Session values are HMAC-SHA-256 authenticated with a minimum 256-bit key and expiry. Cookies are `HttpOnly`, `SameSite=Lax`, path-scoped, and `Secure` for HTTPS public URLs. Uploads require authentication, same origin, and a non-simple custom header; logout is POST plus same-origin validation.
- **Authorization and tenant isolation:** The application exposes no read/list/delete endpoint. Upload paths derive from a signed subject and sanitized username with a 96-bit subject-hash suffix. No cross-user object reference was found.
- **Filesystem handling:** Client filenames are reduced to a base name, reserved separators/control characters are replaced, directory components are constrained, temporary files are private, and hard-link publication atomically avoids overwrite races. No reachable path traversal or symlink upload was found under the documented single-process ownership model.
- **Injection/XSS:** Templates use `html/template`; IdP display names are contextually escaped; browser filenames use `textContent`; there is no SQL, shell, server-side template, reflection, unsafe deserialization, or user-selected outbound URL sink in the application.
- **Secrets and logging:** Required secrets come from the environment; generated configuration is mode `0600`; no credential value is logged by project code; a tracked-file pattern scan found no private key, GitHub token, or AWS access key. No embedded secret was observed in the tracked executable metadata.
- **Cryptography:** Random values use `crypto/rand`; signatures use HMAC-SHA-256 and constant-time verification; no custom encryption or obsolete primitive was found.
- **Browser controls:** CSP, frame denial, MIME sniffing prevention, referrer policy, permissions policy, and contextual output handling are present. Inline CSP allowances are undesirable but no attacker-controlled script/style injection source was reachable, so they are not a standalone finding.
- **Container/system service:** Runtime is a scratch image under UID/GID 65532; uploads are isolated in a dedicated volume. The Linux installer uses a dedicated unprivileged account, restrictive data/config modes, `NoNewPrivileges`, and extensive systemd sandboxing. The LXC is unprivileged.
- **CI/release:** Workflow permissions are least privilege, external actions are pinned to full commit SHAs, tag syntax is validated, tests/vet run before release, release builds are reproducible-oriented (`-trimpath`, static), and checksums are published. No `pull_request_target`, untrusted expression-to-shell interpolation, artifact secret exposure, or long-lived CI credential was found.
- **Dependencies:** `go.sum` integrity verified. Manual advisory research found no published advisory affecting `go-oidc/v3 v3.20.0`, `x/oauth2 v0.36.0`, or `go-jose/v4 v4.1.4` as of the review date.
- **Static assets:** Embedded SVG files contain no script, event handler, foreign object, or external resource reference.

## Not checked

- Live reverse-proxy, DNS, TLS, firewall, IdP tenant/policies, registration rules, storage/filesystem quotas, backup access, monitoring, and incident response configuration.
- Runtime penetration testing against a deployed instance or its real identity provider.
- Provenance and source-level reproducibility of the checked-in `dumpbox` executable beyond `go version -m` metadata inspection.
- Private GitHub repository settings: branch protection, required reviews, tag protection, Dependabot, secret scanning, code scanning, release environment protection, and maintainer MFA.
- Third-party base-image/package registry compromise and vulnerabilities outside public advisory data available on the review date.

## Needs a human

- Confirm whether the IdP permits public self-registration or external/federated identities; this determines whether upload exhaustion should be rated High.
- Confirm the reverse proxy redirects all HTTP to HTTPS, emits HSTS, applies request/concurrency/minimum-rate limits, and does not trust spoofable forwarding headers.
- Define who may upload. Dumpbox authenticates every valid identity from the configured issuer but performs no group, tenant, domain, or explicit allowlist authorization.
- Confirm that people who later access `/var/lib/dumpbox` treat every upload as hostile. The application intentionally accepts arbitrary bytes and does not scan, unpack, render, or serve them; downstream antivirus/content-disarm and safe-viewing requirements depend on that external workflow.
- Confirm Proxmox console permissions. The LXC script deliberately configures passwordless root autologin on the container console; this is acceptable only if every principal with console access is authorized for container root.
- Confirm the data directory is exclusively controlled by the Dumpbox service account. A local actor able to replace user directories with symlinks already has equivalent access to uploaded data and can redirect writes.
- Decide whether 12-hour non-revocable stateless sessions meet incident-response requirements. Rotating `SESSION_SECRET` is currently the only global revocation mechanism.
- Enable and review GitHub code scanning and secret scanning. API access to both alert sets returned `403 Resource not accessible by integration` during this review.

## Validation record

The review used manual source-to-sink tracing before scanner checks. The following checks passed on 2026-07-24:

- `go test -race ./...`
- `go vet ./...`
- `go build ./cmd/dumpbox`
- `bash -n scripts/install.sh scripts/proxmox-lxc.sh`
- `go mod verify`
- `git diff --check`
- tracked-file secret-pattern scan

`govulncheck` was not installed in the repository environment. GitHub code-scanning and secret-scanning alert queries were attempted after manual review and both returned `403 Resource not accessible by integration`; no claim is made that those private alert sets are empty. Public advisory research found no advisory affecting the resolved Go module versions as of the review date.
