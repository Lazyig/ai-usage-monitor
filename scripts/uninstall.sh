#!/bin/sh
# ai-usage-agent uninstaller for Linux / macOS.
#   curl -fsSL https://SERVER/downloads/uninstall.sh | sh                    # service + binary + local state
#   curl -fsSL https://SERVER/downloads/uninstall.sh | sh -s -- --keep-state # keep config/cursors/queue
# Removing the device on the server is a separate step: 기기 관리 → 토큰 폐기 or 완전 삭제.
set -u
KEEP_STATE=0; PREFIX=""
while [ $# -gt 0 ]; do
  case "$1" in
    --keep-state) KEEP_STATE=1; shift;;
    --prefix) PREFIX="$2"; shift 2;;
    *) echo "unknown arg $1" >&2; exit 2;;
  esac
done

bin=""
for c in "${PREFIX:+$PREFIX/ai-usage-agent}" /usr/local/bin/ai-usage-agent "$HOME/.local/bin/ai-usage-agent"; do
  if [ -n "$c" ] && [ -x "$c" ]; then bin="$c"; break; fi
done

if [ -n "$bin" ]; then
  echo "stopping and unregistering the service via $bin"
  "$bin" stop >/dev/null 2>&1 || true
  "$bin" uninstall >/dev/null 2>&1 || true
else
  echo "ai-usage-agent binary not found; cleaning up any service registration anyway"
fi

# Fallbacks for when the binary is already gone.
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user disable --now ai-usage-agent >/dev/null 2>&1 || true
  if [ "$(id -u)" = "0" ]; then systemctl disable --now ai-usage-agent >/dev/null 2>&1 || true; fi
  rm -f "$HOME/.config/systemd/user/ai-usage-agent.service" 2>/dev/null || true
  if [ "$(id -u)" = "0" ]; then rm -f /etc/systemd/system/ai-usage-agent.service 2>/dev/null || true; systemctl daemon-reload >/dev/null 2>&1 || true; fi
fi
if [ "$(uname -s)" = "Darwin" ]; then
  plist="$HOME/Library/LaunchAgents/ai-usage-agent.plist"
  if [ -f "$plist" ]; then
    launchctl bootout "gui/$(id -u)" "$plist" >/dev/null 2>&1 || launchctl unload "$plist" >/dev/null 2>&1 || true
    rm -f "$plist"
  fi
fi
pkill -f 'ai-usage-agent run' >/dev/null 2>&1 || true

if [ -n "$bin" ]; then rm -f "$bin" && echo "removed $bin"; fi

if [ "$KEEP_STATE" = "0" ]; then
  for d in "${XDG_CONFIG_HOME:-$HOME/.config}/ai-usage-agent" "$HOME/Library/Application Support/ai-usage-agent"; do
    if [ -d "$d" ]; then rm -rf "$d" && echo "removed $d"; fi
  done
else
  echo "kept local state (config.json, cursors.json, queue/)"
fi
echo "done. To stop the server from accepting this device's token, revoke or delete it in the dashboard (기기 관리)."
