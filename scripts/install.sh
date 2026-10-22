#!/bin/sh
# ai-usage-agent installer for Linux / macOS.
#   curl -fsSL https://SERVER/downloads/install.sh | sh -s -- --server https://SERVER --token aum_xxx
# Downloads the binary matching this machine from SERVER/downloads/, verifies its
# sha256 against manifest.json, installs it and registers the OS service.
set -eu
SERVER=""; TOKEN=""; PREFIX=""; NO_SERVICE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2;;
    --server=*) SERVER="${1#*=}"; shift;;
    --token) TOKEN="$2"; shift 2;;
    --token=*) TOKEN="${1#*=}"; shift;;
    --prefix) PREFIX="$2"; shift 2;;
    --no-service) NO_SERVICE=1; shift;;
    -h|--help) echo "usage: install.sh --server <url> [--token <token>] [--prefix <directory>] [--no-service]"; exit 0;;
    *) echo "unknown arg $1" >&2; exit 2;;
  esac
done
[ -n "$SERVER" ] || { echo "--server required" >&2; exit 2; }
SERVER="${SERVER%/}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in linux|darwin) ;; *) echo "unsupported OS $os" >&2; exit 1;; esac
arch=$(uname -m)
case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported arch $arch" >&2; exit 1;; esac
name="ai-usage-agent_${os}_${arch}"

if [ -z "$PREFIX" ]; then
  if [ "$(id -u)" = "0" ]; then PREFIX=/usr/local/bin; else PREFIX="$HOME/.local/bin"; fi
fi
mkdir -p "$PREFIX"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
download() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    echo "need curl or wget" >&2; exit 1
  fi
}
echo "downloading $SERVER/downloads/$name"
download "$SERVER/downloads/$name" "$tmp/$name"
download "$SERVER/downloads/manifest.json" "$tmp/manifest.json"
want=$(tr -d '\n ' < "$tmp/manifest.json" | sed -n "s/.*\"name\":\"$name\"[^}]*\"sha256\":\"\([a-f0-9]*\)\".*/\1/p")
[ "${#want}" = 64 ] || { echo "manifest has no valid sha256 for $name" >&2; exit 1; }
if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/$name" | cut -d' ' -f1); else got=$(shasum -a 256 "$tmp/$name" | cut -d' ' -f1); fi
[ "$got" = "$want" ] || { echo "sha256 mismatch: got $got want $want" >&2; exit 1; }
echo "sha256 ok"
# Stop a running service first so the restart picks up the new binary cleanly.
if [ -x "$PREFIX/ai-usage-agent" ]; then "$PREFIX/ai-usage-agent" stop >/dev/null 2>&1 || true; fi
install -m 0755 "$tmp/$name" "$PREFIX/ai-usage-agent"
echo "installed $PREFIX/ai-usage-agent ($("$PREFIX/ai-usage-agent" version 2>/dev/null || echo unknown))"

if [ "$NO_SERVICE" = 0 ] && [ -n "$TOKEN" ]; then
  "$PREFIX/ai-usage-agent" install --server "$SERVER" --token "$TOKEN"
else
  echo "next: $PREFIX/ai-usage-agent install --server $SERVER --token <device token>"
fi
