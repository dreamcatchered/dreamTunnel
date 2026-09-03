#!/usr/bin/env bash
# release.sh — cross-compile dt for all supported platforms
set -e

VERSION="${1:-dev}"
OUT="dist"
mkdir -p "$OUT"

platforms=(
  "windows/amd64"
  "windows/386"
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

for p in "${platforms[@]}"; do
  GOOS="${p%%/*}"
  GOARCH="${p##*/}"
  name="dt-${GOOS}-${GOARCH}"
  [ "$GOOS" = "windows" ] && name="${name}.exe"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w" -o "$OUT/$name" .
done

echo "done: $OUT/"
ls -la "$OUT/"
