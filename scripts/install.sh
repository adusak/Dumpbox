#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="${DUMPBOX_REPOSITORY:-adusak/Dumpbox}"
readonly INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/dumpbox"
readonly CONFIG_FILE="${CONFIG_DIR}/dumpbox.env"
readonly DATA_DIR="/var/lib/dumpbox"
readonly SERVICE_FILE="/etc/systemd/system/dumpbox.service"
readonly UPDATE_COMMAND="${INSTALL_DIR}/update"

fail() {
  echo "Error: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

prompt_value() {
  local variable="$1"
  local label="$2"
  local secret="${3:-false}"
  local value="${!variable:-}"

  if [[ -z "$value" ]]; then
    [[ -r /dev/tty ]] || fail "$variable must be set for a non-interactive installation"
    if [[ "$secret" == "true" ]]; then
      read -r -s -p "$label: " value </dev/tty
      echo >/dev/tty
    else
      read -r -p "$label: " value </dev/tty
    fi
  fi
  [[ -n "$value" ]] || fail "$variable cannot be empty"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "$variable cannot contain newlines"
  printf -v "$variable" '%s' "$value"
}

write_environment_value() {
  local name="$1"
  local value="$2"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s="%s"\n' "$name" "$value"
}

[[ $EUID -eq 0 ]] || fail "run this installer as root"
for command in apt-cache apt-get curl sha256sum tar install systemctl useradd groupadd getent openssl; do
  require_command "$command"
done

apt-get update
if apt-cache show cosign >/dev/null 2>&1; then
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends cosign
fi
require_command cosign

case "$(uname -m)" in
  x86_64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

version="${DUMPBOX_VERSION:-latest}"
if [[ "$version" == "latest" ]]; then
  release_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPOSITORY}/releases/latest")"
  version="${release_url##*/}"
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  fail "invalid release version: $version"

archive="dumpbox_${version#v}_linux_${arch}.tar.gz"
download_url="https://github.com/${REPOSITORY}/releases/download/${version}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

echo "Downloading Dumpbox ${version} for ${arch}..."
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/${archive}" \
  "${download_url}/${archive}"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/checksums.txt" \
  "${download_url}/checksums.txt"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/${archive}.sigstore.json" \
  "${download_url}/${archive}.sigstore.json"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/checksums.txt.sigstore.json" \
  "${download_url}/checksums.txt.sigstore.json"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/update.sh" \
  "${download_url}/update.sh"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/update.sh.sigstore.json" \
  "${download_url}/update.sh.sigstore.json"

certificate_identity="https://github.com/${REPOSITORY}/.github/workflows/release.yml@refs/tags/${version}"
for asset in "$archive" checksums.txt update.sh; do
  cosign verify-blob \
    --bundle "${temporary_dir}/${asset}.sigstore.json" \
    --certificate-identity "$certificate_identity" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    "${temporary_dir}/${asset}"
done

(
  cd "$temporary_dir"
  awk -v archive="$archive" '$2 == archive || $2 == ("./" archive)' checksums.txt |
    grep -F "$archive" |
    sha256sum -c -
)
tar -xzf "${temporary_dir}/${archive}" -C "$temporary_dir"
[[ -f "${temporary_dir}/dumpbox" ]] || fail "release archive does not contain dumpbox"

if ! getent group dumpbox >/dev/null; then
  groupadd --system dumpbox
fi
if ! getent passwd dumpbox >/dev/null; then
  useradd --system --gid dumpbox --home-dir "$DATA_DIR" --shell /usr/sbin/nologin dumpbox
fi

install -d -m 0750 -o root -g dumpbox "$CONFIG_DIR"
install -d -m 0700 -o dumpbox -g dumpbox "$DATA_DIR"
install -m 0755 "${temporary_dir}/dumpbox" "${INSTALL_DIR}/dumpbox"
install -m 0755 "${temporary_dir}/update.sh" "$UPDATE_COMMAND"

if [[ ! -f "$CONFIG_FILE" || "${DUMPBOX_RECONFIGURE:-false}" == "true" ]]; then
  prompt_value BASE_URL "Public URL (for example, https://dumpbox.example.com)"
  prompt_value OIDC_ISSUER_URL "OIDC issuer URL"
  prompt_value OIDC_CLIENT_ID "OIDC client ID"
  prompt_value OIDC_CLIENT_SECRET "OIDC client secret" true
  SESSION_SECRET="${SESSION_SECRET:-$(openssl rand -base64 32)}"

  {
    write_environment_value BASE_URL "$BASE_URL"
    write_environment_value OIDC_ISSUER_URL "$OIDC_ISSUER_URL"
    write_environment_value OIDC_CLIENT_ID "$OIDC_CLIENT_ID"
    write_environment_value OIDC_CLIENT_SECRET "$OIDC_CLIENT_SECRET"
    write_environment_value SESSION_SECRET "$SESSION_SECRET"
    write_environment_value LISTEN_ADDR ":8080"
    write_environment_value METRICS_LISTEN_ADDR ":9090"
    write_environment_value DATA_DIR "$DATA_DIR"
  } >"${temporary_dir}/dumpbox.env"
  install -m 0600 -o root -g root "${temporary_dir}/dumpbox.env" "$CONFIG_FILE"
else
  echo "Keeping existing configuration in ${CONFIG_FILE}."
fi

cat >"$SERVICE_FILE" <<'EOF'
[Unit]
Description=Dumpbox OIDC file drop
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=dumpbox
Group=dumpbox
WorkingDirectory=/var/lib/dumpbox
EnvironmentFile=/etc/dumpbox/dumpbox.env
ExecStart=/usr/local/bin/dumpbox
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateDevices=true
PrivateTmp=true
ProtectClock=true
ProtectControlGroups=true
ProtectHome=true
ProtectHostname=true
ProtectKernelLogs=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectSystem=strict
ReadWritePaths=/var/lib/dumpbox
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable dumpbox.service >/dev/null
if ! systemctl restart dumpbox.service; then
  journalctl -u dumpbox.service --no-pager -n 20 >&2 || true
  fail "Dumpbox failed to start; verify ${CONFIG_FILE} and OIDC connectivity"
fi

echo "Dumpbox ${version} is installed and listening on port 8080."
echo "Configuration: ${CONFIG_FILE}"
echo "Uploads: ${DATA_DIR}"
