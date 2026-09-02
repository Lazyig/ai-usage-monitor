#!/bin/sh
# Cross-compile the agent for every supported OS/arch into $OUT (default dist/)
# and write manifest.json (name, os, arch, size, sha256) next to the binaries.
# Usage: VERSION=0.1.0 scripts/build-agents.sh [outdir]
set -eu
OUT="${1:-dist}"
VERSION="${VERSION:-dev}"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p "$OUT"
rm -f "$OUT"/ai-usage-agent_* "$OUT"/manifest.json

targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"
files=""
for t in $targets; do
  os="${t%/*}"; arch="${t#*/}"
  name="ai-usage-agent_${os}_${arch}"
  [ "$os" = "windows" ] && name="$name.exe"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/agent
  size=$(wc -c < "$OUT/$name" | tr -d ' ')
  sha=$(sha256sum "$OUT/$name" | cut -d' ' -f1)
  files="$files{\"name\":\"$name\",\"os\":\"$os\",\"arch\":\"$arch\",\"size\":$size,\"sha256\":\"$sha\"},"
done
files="${files%,}"
printf '{"version":"%s","built_at":"%s","files":[%s]}\n' "$VERSION" "$BUILT_AT" "$files" > "$OUT/manifest.json"
echo "wrote $OUT/manifest.json"
