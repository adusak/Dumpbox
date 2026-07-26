# Copilot instructions for Dumpbox

Dumpbox is a small, self-hosted, OIDC-only file drop written in Go. Uploads
stream directly to disk into stable per-user folders.

Read [`AGENTS.md`](../AGENTS.md) in the repository root first. It is the single
source of truth for the project vision, goals and non-goals, repository map,
request flow, coding conventions, validation commands, and security
expectations.

Key rules, repeated here because they are easy to miss:

- Keep changes surgical and stay inside the goals and non-goals in `AGENTS.md`.
- Configuration is environment variables only, parsed and validated in
  `internal/dumpbox/config.go`, and documented in the `README.md` table.
- Never join untrusted input into a filesystem path; use the sanitizing helpers
  in `internal/dumpbox/server.go`.
- Updating documentation is part of the change. See the "Documentation is part
  of every change" section of `AGENTS.md`.
- Validate with `gofmt -l .`, `go vet ./...`, `go test -race ./...`,
  `go build ./cmd/dumpbox`, and, for script changes, `bash -n` plus
  `shellcheck --severity=warning` on `scripts/*.sh`.
