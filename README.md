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
the application starts. Dumpbox sends `Strict-Transport-Security` itself when
`BASE_URL` uses `https`; the proxy should also redirect HTTP to HTTPS and apply
request-rate and minimum-transfer-rate limits.

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
| `OIDC_ALLOW_INSECURE_ISSUER` | no | `false` | Allows a plaintext `http` issuer, and only when its host is loopback |
| `MAX_REQUEST_BYTES` | no | `5368709120` | Maximum bytes accepted per upload request |
| `MAX_FILE_BYTES` | no | `5368709120` | Maximum bytes accepted per file |
| `MAX_FILES_PER_REQUEST` | no | `100` | Maximum files accepted per upload request |
| `MAX_CONCURRENT_UPLOADS_PER_USER` | no | `4` | Concurrent uploads allowed per user |
| `MAX_CONCURRENT_UPLOADS` | no | `32` | Concurrent uploads allowed across all users |

`OIDC_ISSUER_URL` must be an absolute `https` URL without userinfo, query, or
fragment. Requests over the limits are rejected with `413`, and requests over the
concurrency caps with `429`. These application limits do not replace storage
quotas: the service enforces no per-user disk quota, so also apply filesystem
quotas or volume size limits to the data directory.

The OIDC scopes are `openid profile email`. User folder names include a sanitized
`preferred_username` followed by a hash of the immutable OIDC `sub` claim. If
`preferred_username` is unavailable, only the hash is used. Files are written
with `0600` permissions, user folders with `0700`, and duplicate filenames
receive a numeric suffix instead of overwriting existing data.

## Build and test

Go 1.25 or newer is required.

```sh
go test ./...
go vet ./...
go build ./cmd/dumpbox
```

The health endpoint is available at `GET /healthz`.

## Install in a Proxmox LXC

The installer creates a passwordless, unprivileged Debian LXC with automatic
root login on its Proxmox console, installs the latest verified Dumpbox release,
configures a dedicated system user and hardened systemd unit, and starts the
service. Run this command as `root` in the Proxmox VE shell:

```sh
bash -c "$(curl -fsSL https://raw.githubusercontent.com/adusak/Dumpbox/main/scripts/proxmox-lxc.sh)"
```

> [!WARNING]
> This bootstrap command, and the `update` command it installs, download a
> script from the mutable `main` branch and execute it as `root` on the Proxmox
> host or in the container. TLS authenticates GitHub, but it does not bind the
> content to a reviewed revision, so a compromise of the repository, a
> maintainer account, or the delivery path at that moment becomes root command
> execution. Review the script before running it, or replace `main` with a
> commit SHA you have reviewed, and set `DUMPBOX_INSTALLER_URL` for `update` to
> the same pinned revision.

The script prompts for the container resources, an optional IPv4 address in
CIDR notation, and required OIDC settings. It uses DHCP on `vmbr0` when the
address is left blank. Every prompt can also be supplied as an environment
variable for unattended installation:

```sh
CTID=120 HOSTNAME=dumpbox CORES=1 MEMORY=512 DISK_SIZE=20 \
IPV4_ADDRESS=192.0.2.10/24 IPV4_GATEWAY=192.0.2.1 \
BRIDGE=vmbr0 TEMPLATE_STORAGE=local ROOT_STORAGE=local-lvm \
BASE_URL=https://dumpbox.example \
OIDC_ISSUER_URL=https://identity.example \
OIDC_CLIENT_ID=dumpbox \
OIDC_CLIENT_SECRET=replace-me \
bash -c "$(curl -fsSL https://raw.githubusercontent.com/adusak/Dumpbox/main/scripts/proxmox-lxc.sh)"
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

The update command preserves existing configuration, verifies the release
checksum, and restarts the service. Existing installations can add the command
by rerunning the Linux installer once.

The Linux installer also works on an existing systemd-based AMD64 or ARM64
Debian/Ubuntu host.

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
