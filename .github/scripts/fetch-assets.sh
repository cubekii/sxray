#!/usr/bin/env bash
# Downloads the packaging assets (GeoIP/GeoSite, Wintun) into ./resources.
# The workflows only restore them from the Actions cache, which is populated by
# scheduled-assets-update.yml. On a fresh fork that cache is empty, so the
# assets are fetched here instead of failing the packaging step.
#
# Usage: fetch-assets.sh geodat|wintun

set -euo pipefail

GEODAT_LIST=(
  'Loyalsoldier v2ray-rules-dat release geoip'
  'Loyalsoldier v2ray-rules-dat release geosite'
)
WINTUN_ARCHS=(amd64 x86 arm64)
WINTUN_VERSION="${WINTUN_VERSION:-0.14.1}"
WINTUN_SHA256="${WINTUN_SHA256:-07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51}"

fetch_geodat() {
  local entry owner repo branch file_name
  for entry in "${GEODAT_LIST[@]}"; do
    read -r owner repo branch file_name <<<"${entry}"
    if [ -s "resources/${file_name}.dat" ]; then
      echo -e "Using cached resources/${file_name}.dat"
      continue
    fi
    echo -e "Downloading https://raw.githubusercontent.com/${owner}/${repo}/${branch}/${file_name}.dat..."
    curl -fL --retry 5 --retry-delay 5 \
      "https://raw.githubusercontent.com/${owner}/${repo}/${branch}/${file_name}.dat" \
      -o "resources/${file_name}.dat"
  done
}

fetch_wintun() {
  local arch missing=false
  for arch in "${WINTUN_ARCHS[@]}"; do
    [ -s "resources/wintun/bin/${arch}/wintun.dll" ] || missing=true
  done
  [ -s "resources/wintun/LICENSE.txt" ] || missing=true

  if [ "${missing}" != true ]; then
    echo -e "Using cached wintun"
    return
  fi

  echo -e "Downloading https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip..."
  curl -fL --retry 5 --retry-delay 5 \
    "https://www.wintun.net/builds/wintun-${WINTUN_VERSION}.zip" -o wintun.zip
  echo "${WINTUN_SHA256}  wintun.zip" | sha256sum --check --status ||
    { echo -e "Digest of wintun.zip mismatch."; exit 1; }
  echo -e "Unpacking wintun..."
  unzip -o wintun.zip -d resources/
  rm -f wintun.zip
}

mkdir -p resources

case "${1:-}" in
  geodat) fetch_geodat ;;
  wintun) fetch_wintun ;;
  *)
    echo "Usage: $0 geodat|wintun" >&2
    exit 1
    ;;
esac