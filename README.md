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
docker compose up -d --build
```

Terminate TLS at a reverse proxy and forward requests to port 8080. `BASE_URL`
must be the public origin, without a path. OIDC discovery must be reachable when
the application starts.

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `OIDC_ISSUER_URL` | yes | | OIDC issuer used for discovery |
| `OIDC_CLIENT_ID` | yes | | OIDC client identifier |
| `OIDC_CLIENT_SECRET` | yes | | OIDC client secret |
| `SESSION_SECRET` | yes | | Base64-encoded signing key of at least 32 bytes |
| `BASE_URL` | no | `http://localhost:8080` | Public application origin |
| `LISTEN_ADDR` | no | `:8080` | HTTP listen address |
| `DATA_DIR` | no | `./data` | Root upload directory |

The OIDC scopes are `openid profile email`. A hash of the immutable OIDC `sub`
claim forms the stable user folder name. Files are written with `0600`
permissions, user folders with `0700`, and duplicate filenames receive a numeric
suffix instead of overwriting existing data.

## Build and test

Go 1.25 or newer is required.

```sh
go test ./...
go vet ./...
go build ./cmd/dumpbox
```

The health endpoint is available at `GET /healthz`.

## Install in a Proxmox LXC

The installer creates an unprivileged Debian LXC, installs the latest verified
Dumpbox release, configures a dedicated system user and hardened systemd unit,
and starts the service. Run this command as `root` in the Proxmox VE shell:

```sh
export GITHUB_TOKEN=github_pat_your_fine_grained_token
bash -c "$(printf 'user = \"x-access-token:%s\"\n' "$GITHUB_TOKEN" |
  curl --config - -fsSL \
  -H "Accept: application/vnd.github.raw+json" \
  "https://api.github.com/repos/adusak/Dumpbox/contents/scripts/proxmox-lxc.sh?ref=main")"
```

The fine-grained token must have read access to this private repository's
contents. It is also used inside the container to download private release
assets.

The script prompts for the container resources and required OIDC settings. It
uses DHCP on `vmbr0` by default. Every prompt can also be supplied as an
environment variable for unattended installation:

```sh
CTID=120 HOSTNAME=dumpbox CORES=1 MEMORY=512 DISK_SIZE=20 \
BRIDGE=vmbr0 TEMPLATE_STORAGE=local ROOT_STORAGE=local-lvm \
BASE_URL=https://dumpbox.example \
OIDC_ISSUER_URL=https://identity.example \
OIDC_CLIENT_ID=dumpbox \
OIDC_CLIENT_SECRET=replace-me \
bash -c "$(printf 'user = \"x-access-token:%s\"\n' "$GITHUB_TOKEN" |
  curl --config - -fsSL \
  -H "Accept: application/vnd.github.raw+json" \
  "https://api.github.com/repos/adusak/Dumpbox/contents/scripts/proxmox-lxc.sh?ref=main")"
```

Register `${BASE_URL}/auth/callback` with the OIDC provider. Terminate TLS at a
reverse proxy and forward requests to the displayed LXC address on port 8080.

Inside the container:

- configuration is stored in `/etc/dumpbox/dumpbox.env`;
- uploads are stored in `/var/lib/dumpbox`;
- logs are available with `journalctl -u dumpbox`;
- the service is managed with `systemctl {status,restart} dumpbox`.

Back up `/etc/dumpbox` and `/var/lib/dumpbox`, or use Proxmox backups. To update,
rerun the Linux installer inside the container; it preserves existing
configuration and verifies the release checksum:

```sh
export GITHUB_TOKEN=github_pat_your_fine_grained_token
bash -c "$(printf 'user = \"x-access-token:%s\"\n' "$GITHUB_TOKEN" |
  curl --config - -fsSL \
  -H "Accept: application/vnd.github.raw+json" \
  "https://api.github.com/repos/adusak/Dumpbox/contents/scripts/install.sh?ref=main")"
```

The Linux installer also works on an existing systemd-based AMD64 or ARM64
Debian/Ubuntu host with `curl`, `jq`, `openssl`, and standard system utilities.

## Releases

Pushing a semantic version tag builds static Linux AMD64 and ARM64 archives,
generates SHA-256 checksums, and publishes a GitHub release:

```sh
git tag v1.0.0
git push origin v1.0.0
```

Release assets use the name
`dumpbox_<version>_linux_<architecture>.tar.gz`. The Proxmox and Linux
installers consume these assets automatically.
