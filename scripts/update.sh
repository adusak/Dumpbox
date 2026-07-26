#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="${DUMPBOX_REPOSITORY:-adusak/Dumpbox}"

fail() {
  echo "Error: $*" >&2
  exit 1
}

[[ $EUID -eq 0 ]] || fail "run update as root"
for command in cosign curl; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

version="${DUMPBOX_VERSION:-latest}"
if [[ "$version" == "latest" ]]; then
  release_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPOSITORY}/releases/latest")"
  version="${release_url##*/}"
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  fail "invalid release version: $version"

download_url="https://github.com/${REPOSITORY}/releases/download/${version}"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/install.sh" \
  "${download_url}/install.sh"
curl -fL --retry 3 --retry-delay 2 -o "${temporary_dir}/install.sh.sigstore.json" \
  "${download_url}/install.sh.sigstore.json"
cosign verify-blob \
  --bundle "${temporary_dir}/install.sh.sigstore.json" \
  --certificate-identity "https://github.com/${REPOSITORY}/.github/workflows/release.yml@refs/tags/${version}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  "${temporary_dir}/install.sh"

DUMPBOX_VERSION="$version" bash "${temporary_dir}/install.sh"
