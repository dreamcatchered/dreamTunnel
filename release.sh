#!/usr/bin/env bash
# release.sh — cross-compile dt for all supported platforms
set -e

VERSION="${1:-dev}"
OUT="dist"
mkdir -p "$OUT"

# Baked-in default token so a fresh install works with zero setup:
#   export DT_DEFAULT_TOKEN="$(./dt token "$DT_PUBLIC_TOKEN")"   # obfuscated form
#   ./release.sh v1.1.1
TOKEN_LDFLAGS=""
if [ -n "${DT_DEFAULT_TOKEN:-}" ]; then
  TOKEN_LDFLAGS="-X main.DefaultPublicToken=${DT_DEFAULT_TOKEN}"
  echo "baking in default public token (len ${#DT_DEFAULT_TOKEN})"
else
  echo "note: DT_DEFAULT_TOKEN is unset — clients will still require 'dt login'"
fi

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
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w $TOKEN_LDFLAGS" -o "$OUT/$name" .
done

echo "done: $OUT/"
ls -la "$OUT/"
