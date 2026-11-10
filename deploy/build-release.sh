#!/usr/bin/env bash
# Builds Spindle for Linux into release/: one file per architecture, with the web app and the stand firmware inside.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/release"
bundle="$root/server/internal/firmware/bundle"

cleanup() { rm -rf "$bundle/app" "$bundle/lib"; }
trap cleanup EXIT

(cd "$root/web" && npm ci --silent && npm run build --silent)

cleanup
cp -R "$root/firmware/app" "$root/firmware/lib" "$bundle/"
find "$bundle" -name __pycache__ -type d -prune -exec rm -rf {} +

mkdir -p "$out"
for arch in amd64 arm64; do
	(cd "$root/server" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
		go build -trimpath -ldflags="-s -w" -o "$out/spindle-linux-$arch" ./cmd/spindle)
done
(cd "$out" && shasum -a 256 spindle-linux-* > SHA256SUMS)
echo "Built into $out:"
ls -l "$out"
