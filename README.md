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

The OIDC scopes are `openid profile email`. A user's display claim and a hash of
the immutable OIDC `sub` claim form the folder name. Files are written with
`0600` permissions, user folders with `0700`, and duplicate filenames receive a
numeric suffix instead of overwriting existing data.

## Build and test

Go 1.25 or newer is required.

```sh
go test ./...
go vet ./...
go build ./cmd/dumpbox
```

The health endpoint is available at `GET /healthz`.
