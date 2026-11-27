#!/bin/sh
(
set -eu

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="rexpresence-$os-$arch"
base=https://github.com/Luqueee/rexpresence/releases/latest/download
work=$(mktemp -d "${TMPDIR:-/tmp}/rexpresence.XXXXXX")
staged=
cleanup() {
  [ -z "$staged" ] || rm -f "$staged"
  rm -rf "$work"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 -o "$work/$asset" "$base/$asset"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 -o "$work/SHA256SUMS" "$base/SHA256SUMS"
expected=$(awk -v asset="$asset" '
  $2 == asset || $2 == "*" asset {
    count++
    if (NF == 2 && length($1) == 64 && $1 !~ /[^0-9a-fA-F]/) hash = tolower($1)
  }
  END { if (count == 1 && hash != "") print hash; else exit 1 }
' "$work/SHA256SUMS") || { echo "Missing, duplicate, or invalid checksum for $asset" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(cd "$work" && sha256sum "$asset")
elif command -v shasum >/dev/null 2>&1; then
  actual=$(cd "$work" && shasum -a 256 "$asset")
else
  echo 'Install sha256sum or shasum to verify the download.' >&2
  exit 1
fi
actual=${actual%% *}
if [ "$actual" != "$expected" ]; then
  echo "SHA-256 verification failed for $asset; existing installation was not changed." >&2
  exit 1
fi

bin="$HOME/.local/bin"
if [ -d "$bin/rexpresence" ]; then
  echo "Cannot replace directory $bin/rexpresence" >&2
  exit 1
fi
mkdir -p "$bin"
staged=$(mktemp "$bin/.rexpresence.XXXXXX")
cp "$work/$asset" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$bin/rexpresence"
staged=
printf 'Installed %s\n' "$bin/rexpresence"
case ":${PATH:-}:" in
  *":$bin:"*) ;;
  *) printf '%s\n' 'Add this directory to PATH in your shell configuration:' '  export PATH="$HOME/.local/bin:$PATH"' ;;
esac
printf '%s\n' 'Open Discord and a terminal in Rex. Check for a running watcher:' \
  '  "$HOME/.local/bin/rexpresence" status' \
  'Start sharing in the background (safe to run again):' \
  '  "$HOME/.local/bin/rexpresence" -d' \
  'Optional: add Discord controls to the Rex command palette:' \
  '  "$HOME/.local/bin/rexpresence" setup'
)
