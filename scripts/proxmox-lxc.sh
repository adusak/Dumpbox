#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="${DUMPBOX_REPOSITORY:-adusak/Dumpbox}"

fail() {
  echo "Error: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required Proxmox command not found: $1"
}

prompt_value() {
  local variable="$1"
  local label="$2"
  local default="${3:-}"
  local secret="${4:-false}"
  local value="${!variable:-}"
  local prompt="$label"

  [[ -n "$default" ]] && prompt+=" [$default]"
  if [[ -z "$value" ]]; then
    [[ -r /dev/tty ]] || fail "$variable must be set for a non-interactive installation"
    if [[ "$secret" == "true" ]]; then
      read -r -s -p "$prompt: " value </dev/tty
      echo >/dev/tty
    else
      read -r -p "$prompt: " value </dev/tty
    fi
  fi
  value="${value:-$default}"
  [[ -n "$value" ]] || fail "$variable cannot be empty"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "$variable cannot contain newlines"
  printf -v "$variable" '%s' "$value"
}

prompt_optional_value() {
  local variable="$1"
  local label="$2"
  local value="${!variable:-}"
  local supplied=false

  [[ -v "$variable" ]] && supplied=true

  if [[ "$supplied" == "false" ]] && { : </dev/tty; } 2>/dev/null; then
    read -r -p "$label: " value </dev/tty
  fi
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "$variable cannot contain newlines"
  printf -v "$variable" '%s' "$value"
}

validate_ipv4_address() {
  local value="$1"
  local variable="$2"
  local octet
  local -a octets

  [[ "$value" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
    fail "$variable must be an IPv4 address"
  IFS=. read -r -a octets <<<"$value"
  for octet in "${octets[@]}"; do
    ((10#$octet <= 255)) || fail "invalid IPv4 address for $variable"
  done
}

write_shell_value() {
  printf '%s=' "$1"
  printf '%q' "$2"
  printf '\n'
}

[[ $EUID -eq 0 ]] || fail "run this script as root on a Proxmox VE host"
for command in pveversion pvesh pct pveam pvesm curl; do
  require_command "$command"
done

version="${DUMPBOX_VERSION:-latest}"
if [[ "$version" == "latest" ]]; then
  release_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPOSITORY}/releases/latest")"
  version="${release_url##*/}"
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  fail "invalid release version: $version"
readonly VERSION="$version"
readonly DOWNLOAD_URL="https://github.com/${REPOSITORY}/releases/download/${VERSION}"

default_ctid="$(pvesh get /cluster/nextid 2>/dev/null || true)"
default_template_storage="$(pvesm status -content vztmpl | awk 'NR > 1 && $3 == "active" { print $1; exit }')"
default_root_storage="$(pvesm status -content rootdir | awk 'NR > 1 && $3 == "active" { print $1; exit }')"

prompt_value CTID "Container ID" "$default_ctid"
prompt_value CT_HOSTNAME "Container hostname" "dumpbox"
prompt_value CORES "CPU cores" "1"
prompt_value MEMORY "Memory in MiB" "512"
prompt_value DISK_SIZE "Disk size in GiB" "8"
prompt_value BRIDGE "Network bridge" "vmbr0"
prompt_optional_value IPV4_ADDRESS "IPv4 address in CIDR notation (leave blank for DHCP)"
IPV4_GATEWAY="${IPV4_GATEWAY:-}"
if [[ -n "$IPV4_ADDRESS" ]]; then
  prompt_value IPV4_GATEWAY "IPv4 gateway"
fi
prompt_value TEMPLATE_STORAGE "Template storage" "$default_template_storage"
prompt_value ROOT_STORAGE "Container storage" "$default_root_storage"
prompt_value BASE_URL "Public Dumpbox URL (for example, https://dumpbox.example.com)"
prompt_value OIDC_ISSUER_URL "OIDC issuer URL"
prompt_value OIDC_CLIENT_ID "OIDC client ID" "dumpbox"
prompt_value OIDC_CLIENT_SECRET "OIDC client secret" "" true

[[ "$CTID" =~ ^[1-9][0-9]*$ ]] || fail "CTID must be a positive integer"
[[ "$CORES" =~ ^[1-9][0-9]*$ ]] || fail "CORES must be a positive integer"
[[ "$MEMORY" =~ ^[1-9][0-9]*$ ]] || fail "MEMORY must be a positive integer"
[[ "$DISK_SIZE" =~ ^[1-9][0-9]*$ ]] || fail "DISK_SIZE must be a positive integer"
[[ "$CT_HOSTNAME" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || fail "invalid hostname"
[[ "$BRIDGE" =~ ^[A-Za-z0-9_.:-]+$ ]] || fail "invalid network bridge"
if [[ -n "$IPV4_ADDRESS" ]]; then
  [[ "$IPV4_ADDRESS" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[12][0-9]|3[0-2])$ ]] ||
    fail "IPV4_ADDRESS must be an IPv4 address in CIDR notation"
  validate_ipv4_address "${IPV4_ADDRESS%/*}" IPV4_ADDRESS
  validate_ipv4_address "$IPV4_GATEWAY" IPV4_GATEWAY
elif [[ -n "$IPV4_GATEWAY" ]]; then
  fail "IPV4_GATEWAY requires IPV4_ADDRESS"
fi
[[ -n "$TEMPLATE_STORAGE" ]] || fail "no active storage supports container templates"
[[ -n "$ROOT_STORAGE" ]] || fail "no active storage supports container root directories"
if pct status "$CTID" >/dev/null 2>&1; then
  fail "container ${CTID} already exists"
fi

debian_version="${DEBIAN_VERSION:-13}"
echo "Refreshing container template catalog..."
pveam update >/dev/null
template="$(
  pveam available --section system |
    awk -v version="$debian_version" '$2 ~ ("^debian-" version "-standard_") { print $2 }' |
    sort -V |
    tail -n 1
)"
[[ -n "$template" ]] || fail "no Debian ${debian_version} standard template is available"

if ! pveam list "$TEMPLATE_STORAGE" | awk 'NR > 1 { print $1 }' |
  grep -Fqx "${TEMPLATE_STORAGE}:vztmpl/${template}"; then
  echo "Downloading ${template}..."
  pveam download "$TEMPLATE_STORAGE" "$template"
fi

echo "Creating unprivileged LXC ${CTID}..."
network_ip="${IPV4_ADDRESS:-dhcp}"
network_gateway=""
if [[ -n "$IPV4_ADDRESS" ]]; then
  network_gateway=",gw=${IPV4_GATEWAY}"
fi
pct create "$CTID" "${TEMPLATE_STORAGE}:vztmpl/${template}" \
  --hostname "$CT_HOSTNAME" \
  --cores "$CORES" \
  --memory "$MEMORY" \
  --swap 512 \
  --rootfs "${ROOT_STORAGE}:${DISK_SIZE}" \
  --net0 "name=eth0,bridge=${BRIDGE},ip=${network_ip}${network_gateway},type=veth" \
  --unprivileged 1 \
  --onboot 1 \
  --start 1

pct exec "$CTID" -- passwd --delete root
pct exec "$CTID" -- bash -c '
  set -Eeuo pipefail
  override=/etc/systemd/system/container-getty@1.service.d/override.conf
  mkdir -p "${override%/*}"
  cat >"$override" <<EOF
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin root --noclear --keep-baud - 115200,38400,9600 \$TERM
EOF
  systemctl daemon-reload
  systemctl restart container-getty@1.service
'

installation_complete=false
cleanup() {
  rm -f "${configuration_file:-}"
  if [[ "$installation_complete" != "true" ]]; then
    echo "Installation failed. Container ${CTID} was left in place for troubleshooting." >&2
  fi
}
trap cleanup EXIT

echo "Waiting for container networking..."
pct exec "$CTID" -- bash -c \
  'for attempt in {1..30}; do apt-get update && exit 0; sleep 2; done; exit 1'
pct exec "$CTID" -- apt-get install -y --no-install-recommends ca-certificates curl openssl

configuration_file="$(mktemp)"
chmod 600 "$configuration_file"
{
  write_shell_value BASE_URL "$BASE_URL"
  write_shell_value OIDC_ISSUER_URL "$OIDC_ISSUER_URL"
  write_shell_value OIDC_CLIENT_ID "$OIDC_CLIENT_ID"
  write_shell_value OIDC_CLIENT_SECRET "$OIDC_CLIENT_SECRET"
  write_shell_value DUMPBOX_VERSION "$VERSION"
} >"$configuration_file"

pct push "$CTID" "$configuration_file" /root/dumpbox-install.env --perms 0600
# The variables in this command are expanded inside the container.
# shellcheck disable=SC2016
pct exec "$CTID" -- env \
  DUMPBOX_REPOSITORY="$REPOSITORY" \
  DUMPBOX_VERSION="$VERSION" \
  DUMPBOX_DOWNLOAD_URL="$DOWNLOAD_URL" \
  bash -c '
  set -Eeuo pipefail
  set -a
  source /root/dumpbox-install.env
  set +a
  trap "rm -f /root/dumpbox-install.env" EXIT
  temporary_dir="$(mktemp -d)"
  trap "rm -rf \"$temporary_dir\" /root/dumpbox-install.env" EXIT
  curl -fL --retry 3 --retry-delay 2 -o "$temporary_dir/install.sh" \
    "$DUMPBOX_DOWNLOAD_URL/install.sh"
  bash "$temporary_dir/install.sh"
'

installation_complete=true
container_ip="$(pct exec "$CTID" -- hostname -I | awk '{ print $1 }')"
echo
echo "Dumpbox LXC ${CTID} is ready."
echo "Container address: http://${container_ip}:8080"
echo "OIDC callback URL: ${BASE_URL%/}/auth/callback"
echo "Terminate TLS at a reverse proxy and forward it to the container address."
