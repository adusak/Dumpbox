# AGENTS.md

Guidance for AI agents and humans working in this repository. Read this file
before making changes.

## Vision

Dumpbox is a small, self-hosted file drop that anyone can run on a modest box
and trust with uploads from a known set of identity-provider users. Every design
decision favours a service that stays easy to audit, easy to deploy, and boring
to operate.

## Goals

- **Authentication is only OIDC.** There are no local accounts, passwords,
  invite links, or API tokens. A user is whoever the OIDC provider says they
  are, identified by the immutable `sub` claim.
- **Uploads stream to disk.** Request bodies are never buffered in memory, so
  large files work on small machines.
- **Users are isolated by folder.** Each user gets a stable, sanitized directory
  and files are never overwritten; duplicate names get a numeric suffix.
- **Safe defaults.** Restrictive file modes, signed cookies, same-origin checks,
  security headers, and explicit byte, file-count, and concurrency limits ship enabled.
- **Minimal dependencies.** The standard library first; a third-party module is
  added only when the alternative is hand-rolled security-sensitive code.
- **Small surface.** A handful of routes, one binary, one configuration source
  (environment variables). New features must justify the added surface.

## Non-goals

- File listing, browsing, sharing links, deletion, or a management UI.
- Databases, background workers, or clustering. State lives on the filesystem.
- Per-user disk quotas — the operator applies filesystem quotas instead.
- Terminating TLS. A reverse proxy in front of Dumpbox does that.

## Repository map

| Path | Purpose |
| --- | --- |
| `cmd/dumpbox/main.go` | Entrypoint: loads config, OIDC discovery, HTTP server with timeouts, graceful shutdown |
| `internal/dumpbox/config.go` | `Config` and `LoadConfig`; all environment variable parsing and validation |
| `internal/dumpbox/server.go` | Routes, handlers, auth middleware, multipart upload streaming, filename and directory sanitizing, security headers |
| `internal/dumpbox/session.go` | HMAC-signed session and auth-request cookie payloads |
| `internal/dumpbox/limits.go` | Per-user byte/file quotas and concurrent upload slots |
| `internal/dumpbox/hash.go` | Hash of the OIDC `sub` used in folder names |
| `internal/dumpbox/page.go` | The single HTML page template |
| `internal/dumpbox/assets.go`, `assets/` | Embedded logo and favicon |
| `internal/dumpbox/server_test.go` | Handler, upload, sanitizing, and limit tests |
| `scripts/install.sh` | Linux/systemd installer, also used by the `update` command |
| `scripts/proxmox-lxc.sh` | Proxmox VE bootstrap that creates an LXC and runs the installer |
| `Dockerfile`, `compose.yaml` | Container image and the published-image compose deployment |
| `.github/workflows/ci.yml` | gofmt, `go vet`, `go test -race`, build, `bash -n` and shellcheck |
| `.github/workflows/release.yml` | Tag-driven archives, checksums, GitHub release, GHCR image |
| `docs/security-review/` | Dated security review reports |

## Request flow

1. `GET /` — unauthenticated visitors get the login page; authenticated ones get
   the upload page.
2. `GET /login` — starts the OIDC flow and stores the state and nonce in a
   short-lived signed `dumpbox_auth` cookie.
3. `GET /auth/callback` — verifies state and the ID token, then issues the signed
   `dumpbox_session` cookie.
4. `POST /` (authenticated, same-origin) — streams multipart parts into the
   user's directory under the configured limits.
5. `POST /logout` clears the session; `GET /healthz` is unauthenticated.

## Conventions

- Go 1.25 or newer; code must be `gofmt`-clean.
- Application code lives under `internal/` so nothing becomes a public API.
- Configuration comes from environment variables only, parsed in `config.go`.
  A new setting needs a default, validation, and a README table row.
- Handlers return JSON errors via `writeJSON`; internal failures are logged with
  `slog` and reported to the client without detail.
- Never log secrets, tokens, cookie values, or full file contents.
- Any path segment derived from user or provider input must pass through the
  sanitizing helpers in `server.go`; never join untrusted input into a path.
- Shell scripts are `set -Eeuo pipefail`, POSIX-friendly where practical, and must
  pass `shellcheck --severity=warning`.
- Keep changes surgical. Prefer extending an existing file over adding one.

## Documentation is part of every change

Treat documentation as code: a change is incomplete until the docs match it.
Before finishing any task, update the affected documents:

- `README.md` — new or changed environment variables (keep the configuration
  table accurate), routes, limits, deployment or install steps, release process,
  and build/test commands.
- `AGENTS.md` — new packages, files, routes, or conventions, and any change to
  the goals or non-goals above.
- `scripts/*.sh` help text and prompts when installer inputs change.
- `compose.yaml` and `Dockerfile` comments when the runtime contract changes.
- `docs/security-review/` — add a new dated report rather than editing an old
  one; existing reports are a historical record.

If a change genuinely needs no documentation update, say so explicitly in the
pull request description.

## Validate before finishing

```sh
gofmt -l .
go vet ./...
go test -race ./...
go build ./cmd/dumpbox
bash -n scripts/install.sh scripts/proxmox-lxc.sh
shellcheck --severity=warning scripts/install.sh scripts/proxmox-lxc.sh
```

These mirror `.github/workflows/ci.yml`. Run at least the Go commands for Go
changes and the shell commands for script changes.

## Security expectations

Dumpbox accepts unauthenticated internet traffic on its login routes and
authenticated file writes everywhere else, so review changes with that in mind:
keep the same-origin check on state-changing requests, keep cookies `HttpOnly`
and `SameSite`, keep constant-time comparison for secrets, keep the `0600`/`0700`
file modes, and keep the byte, file-count, and concurrency limits enforced before any data is
written to disk.
