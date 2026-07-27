# Dumpbox

Dumpbox is a small, self-hosted file drop backed exclusively by OpenID Connect.
Uploads stream directly to disk and are separated into stable per-user folders.

## Run with Docker Compose

Register an OIDC client with this callback URL:

```text
https://dumpbox.example/auth/callback
```

Create a `.env` file:

```dotenv
BASE_URL=https://dumpbox.example
OIDC_ISSUER_URL=https://identity.example
OIDC_CLIENT_ID=dumpbox
OIDC_CLIENT_SECRET=replace-me
SESSION_SECRET=replace-with-output-from-command-below
```

Generate the session signing key and start the service:

```sh
openssl rand -base64 32
docker compose up -d
```

The compose file runs the published multi-architecture image
`ghcr.io/adusak/dumpbox:latest`. Set `DUMPBOX_VERSION` to pin a released tag,
for example `DUMPBOX_VERSION=1.0.0`, or run `docker compose up -d --build` to
build the image from this checkout instead. The service binds only to
`127.0.0.1:8080`, uses a read-only root filesystem, drops all capabilities,
enables `no-new-privileges`, and applies memory and process limits.

Terminate TLS at a reverse proxy and forward requests to port 8080. `BASE_URL`
must be the public origin, without a path. OIDC discovery must be reachable when
the application starts. Dumpbox sends `Strict-Transport-Security` itself when
`BASE_URL` uses `https`; the proxy should also redirect HTTP to HTTPS and apply
request-rate and minimum-transfer-rate limits.
Configure the proxy to terminate uploads that fall below the minimum transfer
rate or exceed its request-body timeout; Dumpbox deliberately has no
application-level upload deadline so large legitimate uploads can stream.

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `OIDC_ISSUER_URL` | yes | | OIDC issuer used for discovery |
| `OIDC_CLIENT_ID` | yes | | OIDC client identifier |
| `OIDC_CLIENT_SECRET` | yes | | OIDC client secret |
| `SESSION_SECRET` | yes | | Base64-encoded signing key of at least 32 bytes |
| `BASE_URL` | no | `http://localhost:8080` | Public application origin |
| `LISTEN_ADDR` | no | `:8080` | HTTP listen address |
| `METRICS_LISTEN_ADDR` | no | `:9090` | Prometheus metrics listen address |
| `DATA_DIR` | no | `./data` | Root upload directory |
| `OIDC_ALLOW_INSECURE_ISSUER` | no | `false` | Allows a plaintext `http` issuer, and only when its host is loopback |
| `MAX_REQUEST_BYTES` | no | `5368709120` (5 GiB) | Maximum bytes accepted per upload request |
| `MAX_FILE_BYTES` | no | `5368709120` (5 GiB) | Maximum bytes accepted per file |
| `MAX_BYTES_PER_USER` | no | `21474836480` (20 GiB) | Cumulative storage allowed per OIDC identity; `0` disables this limit |
| `MAX_FILES_PER_REQUEST` | no | `100` | Maximum files accepted per upload request |
| `MAX_FILES_PER_USER` | no | `10000` | Cumulative files allowed per OIDC identity |
| `MAX_CONCURRENT_UPLOADS_PER_USER` | no | `4` | Concurrent uploads allowed per user |
| `MAX_CONCURRENT_UPLOADS` | no | `32` | Concurrent uploads allowed across all users |

`OIDC_ISSUER_URL` must be an absolute `https` URL without userinfo, query, or
fragment. Requests over the per-request limits are rejected with `413`, users
over their cumulative byte or file-count limit with `507`, and requests over the
concurrency caps with `429`. Dumpbox rebuilds both per-identity counters from
`DATA_DIR` at startup, so these limits work regardless of where the directory is
stored. Filesystem byte and inode quotas remain recommended as defense in depth
and to protect against aggregate use by many identities.

The OIDC scopes are `openid profile email`. User folder names include a sanitized
`preferred_username` followed by a hash of the immutable OIDC `sub` claim. If
`preferred_username` is unavailable, only the hash is used. Files are written
with `0600` permissions, user folders with `0700`, and duplicate filenames
receive a numeric suffix instead of overwriting existing data.

Dumpbox treats every identity that the configured OIDC client authenticates as
authorized to upload. Restrict assignment to that client in the identity
provider, for example to the intended access group, and disable public or
unrestricted client access. Dumpbox does not independently inspect group claims.

## Build and test

Go 1.25 or newer is required.

```sh
go test ./...
go vet ./...
go build ./cmd/dumpbox
```

The health endpoint is available at `GET /healthz` on the application port.
Prometheus metrics are available at `GET /metrics` on the separate metrics
listener (port 9090 by default), including Go and process metrics plus:

- `dumpbox_uploaded_files_total{user}` and
  `dumpbox_uploaded_bytes_total{user}` for successfully stored data;
- `dumpbox_upload_requests_total{user,code}` for upload outcomes;
- `dumpbox_upload_duration_seconds` and `dumpbox_active_uploads`.

The `user` label is the first 24 hexadecimal characters of the SHA-256 hash of
the immutable OIDC subject. It matches the suffix of that user's data directory,
allowing operators to correlate usage without exporting usernames. The metrics
endpoint is unauthenticated for Prometheus scraping; restrict the metrics port
to the monitoring network because it exposes operational data.

## Contributing

[`AGENTS.md`](AGENTS.md) describes the project vision, goals and non-goals,
repository layout, request flow, coding conventions, and the checks to run
before opening a pull request. AI assistants and human contributors should both
start there; updating the documentation is part of every change.

## Install in a Proxmox LXC

The installer creates a passwordless, unprivileged Debian LXC with automatic
root login on its Proxmox console, installs the selected Dumpbox release,
configures a dedicated system user and hardened systemd unit, and starts the
service. Choose a release, then run the script as `root` in the Proxmox VE
shell:

```sh
curl -fLO https://github.com/adusak/Dumpbox/releases/latest/download/proxmox-lxc.sh
bash proxmox-lxc.sh
```

That is the complete LXC setup path; no separate Cosign setup on the Proxmox
host is required. Releases containing this installer also omit Cosign from the
container. Release assets are signed separately for operators who want to
verify them before installation. To pin a version, download `proxmox-lxc.sh`
from that version's release page and run it with `DUMPBOX_VERSION=v1.2.3`.

The script prompts for the container resources, an optional IPv4 address in
CIDR notation, and required OIDC settings. It uses DHCP on `vmbr0` when the
address is left blank. Every prompt can also be supplied as an environment
variable for unattended installation:

```sh
CTID=120 CT_HOSTNAME=dumpbox CORES=1 MEMORY=512 DISK_SIZE=20 \
IPV4_ADDRESS=192.0.2.10/24 IPV4_GATEWAY=192.0.2.1 \
BRIDGE=vmbr0 TEMPLATE_STORAGE=local ROOT_STORAGE=local-lvm \
BASE_URL=https://dumpbox.example \
OIDC_ISSUER_URL=https://identity.example \
OIDC_CLIENT_ID=dumpbox \
OIDC_CLIENT_SECRET=replace-me \
DUMPBOX_VERSION=v1.2.3 bash proxmox-lxc.sh
```

Register `${BASE_URL}/auth/callback` with the OIDC provider. Terminate TLS at a
reverse proxy and forward requests to the displayed LXC address on port 8080.

Inside the container:

- configuration is stored in `/etc/dumpbox/dumpbox.env`;
- uploads are stored in `/var/lib/dumpbox`;
- logs are available with `journalctl -u dumpbox`;
- the service is managed with `systemctl {status,restart} dumpbox`.

Back up `/etc/dumpbox` and `/var/lib/dumpbox`, or use Proxmox backups. To install
the latest release from inside the container, run:

```sh
update
```

The update command preserves existing configuration, resolves a release tag,
downloads the installer from that release, validates the release archive
against its published SHA-256 checksum, and restarts the service. Set
`DUMPBOX_VERSION=v1.2.3 update` to select an exact release. Existing
installations can add the command by running the Linux installer once.

The Linux installer also works on an existing systemd-based AMD64 or ARM64
Debian/Ubuntu host:

```sh
curl -fLO https://github.com/adusak/Dumpbox/releases/latest/download/install.sh
sudo bash install.sh
```

The installer and `update` command do not require Cosign. Existing installations
whose updater verifies Sigstore bundles remain compatible because releases
continue to publish those bundles.

### Optional release verification

To verify a downloaded installer, install Cosign on the machine where you
download it and run:

```sh
VERSION=v1.0.6
REPOSITORY=adusak/Dumpbox
RELEASE_URL="https://github.com/${REPOSITORY}/releases/download/${VERSION}"
curl -fLO "${RELEASE_URL}/install.sh"
curl -fLO "${RELEASE_URL}/install.sh.sigstore.json"
cosign verify-blob \
  --bundle install.sh.sigstore.json \
  --certificate-identity "https://github.com/${REPOSITORY}/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  install.sh
sudo env DUMPBOX_VERSION="$VERSION" bash install.sh
```

The same command can verify `proxmox-lxc.sh` by downloading its matching bundle
and replacing the filename in both places. Do not execute an asset if
verification fails.

## Releases

Pushing a semantic version tag builds static Linux AMD64 and ARM64 archives,
generates SHA-256 checksums, signs the archives, checksums, and installers with
Sigstore keyless signing, publishes them and their verification bundles in a
GitHub release, and pushes a multi-architecture container image to GitHub
Container Registry:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Release assets use the name
`dumpbox_<version>_linux_<architecture>.tar.gz`. The Proxmox and Linux
installers consume these assets automatically. The image is published as
`ghcr.io/adusak/dumpbox` and tagged with the full version, the major and minor
version, and `latest` for stable releases. Pre-release tags (for example
`v1.0.0-rc.1`) are not tagged `latest`.
