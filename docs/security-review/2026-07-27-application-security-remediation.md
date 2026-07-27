# Application security remediation review

**Date:** 2026-07-27  
**Scope:** Remediation of the Medium inode-exhaustion finding in `2026-07-27-application-security.md`.  
**Result:** Remediated.

## Remediation assessment

Dumpbox now limits each OIDC identity to 10,000 stored regular files by default through `MAX_FILES_PER_USER`. Configuration rejects non-positive values. At startup, the application rebuilds both byte and regular-file counts from every recognized user directory, combining directories by the immutable subject-hash suffix.

Before `storePart` creates a temporary file, it atomically reserves a file slot under the same quota mutex used for accounting. The reservation remains after successful hard-link publication and is released on all error and panic paths. Exceeding either the byte or file-count quota returns `507 Insufficient Storage`. This closes the zero-byte-file path identified in the review.

Filesystem byte and inode quotas remain necessary defense in depth because application counters cannot constrain aggregate use across many authorized identities or writes made outside Dumpbox.

## Verification

Tests cover:

- repeated zero-byte uploads reaching the per-user file limit;
- atomic concurrent reservation of the final file slot;
- reservation rollback after a failed upload;
- reconstruction of byte and file counts at startup;
- validation and wiring of `MAX_FILES_PER_USER`.

The following checks passed:

- `gofmt -l .`
- `go vet ./...`
- `go test -race ./...`
- `go build ./cmd/dumpbox`
- changed-file secret-pattern and whitespace checks

## Residual risk

The two Low findings from the application review—slow uploads holding concurrency slots and copied sessions remaining valid after logout—are unchanged and remain open.
