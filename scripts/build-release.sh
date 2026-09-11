#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
version=${1:-dev}
if [[ ! "$version" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
  echo 'Version must contain only letters, digits, dots, underscores and hyphens.' >&2
  exit 1
fi

name="attic-${version}-linux-amd64"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/$name" dist/release

pnpm install --frozen-lockfile
pnpm build:embed
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -trimpath -buildvcs=false -ldflags='-s -w' \
  -o "$stage/$name/attic" ./cmd/attic
cp -R migrations "$stage/$name/"
cp docs/binary-install.md "$stage/$name/README.md"

tar -czf "dist/release/$name.tar.gz" -C "$stage" "$name"
(
  cd dist/release
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$name.tar.gz" > "$name.tar.gz.sha256"
  else
    shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"
  fi
)
echo "Built dist/release/$name.tar.gz"
