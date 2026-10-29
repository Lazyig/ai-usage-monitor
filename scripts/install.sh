#!/bin/sh
# ai-usage-agent installer for Linux / macOS.
#   curl -fsSL https://SERVER/downloads/install.sh | sh -s -- --server https://SERVER --token aum_xxx
# Extra Claude Code config directories: --claude-dir DIR, once per directory
# (default: $CLAUDE_CONFIG_DIR, read by the agent itself).
# Update an installed agent, keeping its config.json (server, token, folders):
#   curl -fsSL https://SERVER/downloads/install.sh | sh -s -- --server https://SERVER --update
# Run it as the account that installed the agent (sudo if that was root).
# Downloads the binary matching this machine from SERVER/downloads/, verifies its
# sha256 against manifest.json, installs it and registers the OS service.
set -eu
SERVER=""; TOKEN=""; PREFIX=""; NO_SERVICE=0; UPDATE=0; ALLOW_HTTP=0
# Newline separated: POSIX sh has no arrays, and a path never holds a newline.
CLAUDE_DIRS=""
nl='
'
while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2;;
    --server=*) SERVER="${1#*=}"; shift;;
    --token) TOKEN="$2"; shift 2;;
    --token=*) TOKEN="${1#*=}"; shift;;
    --prefix) PREFIX="$2"; shift 2;;
    --claude-dir) CLAUDE_DIRS="$CLAUDE_DIRS$nl$2"; shift 2;;
    --claude-dir=*) CLAUDE_DIRS="$CLAUDE_DIRS$nl${1#*=}"; shift;;
    --no-service) NO_SERVICE=1; shift;;
    --update) UPDATE=1; shift;;
    --allow-http) ALLOW_HTTP=1; shift;;
    -h|--help) echo "usage: install.sh --server <url> [--token <token> | --update] [--claude-dir <directory>]... [--prefix <directory>] [--no-service] [--allow-http]"; exit 0;;
    *) echo "unknown arg $1" >&2; exit 2;;
  esac
done
[ -n "$SERVER" ] || { echo "--server required" >&2; exit 2; }
SERVER="${SERVER%/}"
# Plain http would carry the binary, its checksum and the device token in the
# clear, so anyone on the path could swap the agent. Only this machine itself
# (localhost, 127.0.0.1, [::1]) is exempt; anything else needs --allow-http.
case "$SERVER" in
  https://*) ;;
  http://localhost|http://localhost:*|http://localhost/*|http://127.0.0.1|http://127.0.0.1:*|http://127.0.0.1/*|http://\[::1\]|http://\[::1\]:*|http://\[::1\]/*) ;;
  http://*)
    if [ "$ALLOW_HTTP" != 1 ]; then
      echo "refusing plain $SERVER: the binary and the device token would cross the network unencrypted. Use https://, or pass --allow-http." >&2
      exit 2
    fi
    echo "warning: plain http server, the binary and the device token are not encrypted" >&2;;
  *) echo "--server must start with https:// (or http:// for this machine)" >&2; exit 2;;
esac

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in linux|darwin) ;; *) echo "unsupported OS $os" >&2; exit 1;; esac
arch=$(uname -m)
case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported arch $arch" >&2; exit 1;; esac
name="ai-usage-agent_${os}_${arch}"

if [ -z "$PREFIX" ]; then
  if [ "$(id -u)" = "0" ]; then PREFIX=/usr/local/bin; else PREFIX="$HOME/.local/bin"; fi
fi
# An update replaces an existing agent in place. Finding none here means the
# wrong account or prefix (a root install lives in /usr/local/bin): stop before
# touching anything rather than install a second, unconfigured copy.
if [ "$UPDATE" = 1 ] && [ -z "$TOKEN" ] && [ ! -x "$PREFIX/ai-usage-agent" ]; then
  echo "--update: no agent at $PREFIX/ai-usage-agent. Run as the account that installed it (sudo if root), pass --prefix, or install with --token." >&2
  exit 2
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
# Stop a running service first so the restart picks up the new binary cleanly,
# remembering that it ran: a run that does not re-register it starts it again.
WAS_RUNNING=0
if [ -x "$PREFIX/ai-usage-agent" ]; then
  if "$PREFIX/ai-usage-agent" status --timeout 2s 2>/dev/null | grep -q '^service *running'; then WAS_RUNNING=1; fi
  "$PREFIX/ai-usage-agent" stop >/dev/null 2>&1 || true
fi
install -m 0755 "$tmp/$name" "$PREFIX/ai-usage-agent"
echo "installed $PREFIX/ai-usage-agent ($("$PREFIX/ai-usage-agent" version 2>/dev/null || echo unknown))"

# One --claude-dir per directory. Globbing is off while splitting, so a quoted
# pattern such as '~/.claude-*' reaches the agent as typed. An update passes no
# token: `install` keeps the one in config.json and re-registers the service.
set -- install --server "$SERVER"
[ -n "$TOKEN" ] && set -- "$@" --token "$TOKEN"
[ "$ALLOW_HTTP" = 1 ] && set -- "$@" --allow-http
hint=""
set -f; old_ifs=$IFS; IFS=$nl
for d in $CLAUDE_DIRS; do set -- "$@" --claude-dir "$d"; hint="$hint --claude-dir '$d'"; done
IFS=$old_ifs; set +f
if [ "$NO_SERVICE" = 0 ] && { [ -n "$TOKEN" ] || [ "$UPDATE" = 1 ]; }; then
  "$PREFIX/ai-usage-agent" "$@"
else
  if [ "$WAS_RUNNING" = 1 ]; then
    "$PREFIX/ai-usage-agent" start || { echo "could not restart the ai-usage-agent service: run $PREFIX/ai-usage-agent start" >&2; exit 1; }
    echo "restarted the ai-usage-agent service with the new binary"
  elif [ "$UPDATE" = 1 ]; then
    echo "updated; restart the agent you run yourself to use the new version"
  fi
  [ "$UPDATE" = 1 ] || echo "next: $PREFIX/ai-usage-agent install --server $SERVER --token <device token>$hint"
fi
