#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
export GOTOOLCHAIN="go$(<.go-version)"
mkdir -p dist

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  name="rexpresence-$os-$arch"
  if [[ $os == windows ]]; then
    name+=.exe
  fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -buildvcs=false -o "dist/$name" ./cmd/rexpresence
done
cp integrations/rex/rexpresence.lua scripts/install.sh scripts/install.ps1 LICENSE dist/
{
  printf '%s\n\n' 'Go standard library and runtime'
  cat "$(go env GOROOT)/LICENSE"
  for module in github.com/Microsoft/go-winio golang.org/x/sys; do
    printf '\n%s\n\n' "$(go list -m -f '{{.Path}} {{.Version}}' "$module")"
    cat "$(go list -m -f '{{.Dir}}' "$module")/LICENSE"
  done
} > dist/THIRD_PARTY_NOTICES.txt
(
  cd dist
  sha256sum rexpresence-darwin-arm64 rexpresence-darwin-amd64 \
    rexpresence-linux-arm64 rexpresence-linux-amd64 \
    rexpresence-windows-amd64.exe rexpresence.lua install.sh install.ps1 LICENSE THIRD_PARTY_NOTICES.txt > SHA256SUMS
)
