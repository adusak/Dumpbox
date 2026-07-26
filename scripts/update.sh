#!/usr/bin/env bash
set -Eeuo pipefail

readonly REPOSITORY="${DUMPBOX_REPOSITORY:-adusak/Dumpbox}"

fail() {
  echo "Error: $*" >&2
  exit 1
}

[[ $EUID -eq 0 ]] || fail "run update as root"
command -v curl >/dev/null 2>&1 || fail "required command not found: curl"

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

DUMPBOX_VERSION="$version" bash "${temporary_dir}/install.sh"
