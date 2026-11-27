#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
testdir=$(mktemp -d "${TMPDIR:-/tmp}/rexpresence-test.XXXXXX")
trap 'rm -rf "$testdir"' 0
trap 'exit 1' HUP INT TERM
mkdir -p "$testdir/commands" "$testdir/fixtures" "$testdir/temp" "$testdir/home with spaces"

cat > "$testdir/commands/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf '%s\n' "$TEST_OS" ;;
  -m) printf '%s\n' "$TEST_ARCH" ;;
  *) exit 1 ;;
esac
EOF
cat > "$testdir/commands/curl" <<'EOF'
#!/bin/sh
out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    --proto|--proto-redir) shift 2 ;;
    --fail|--silent|--show-error|--location|--tlsv1.2) shift ;;
    https://github.com/Luqueee/rexpresence/releases/latest/download/*) url=$1; shift ;;
    *) echo "Unexpected curl argument: $1" >&2; exit 1 ;;
  esac
done
[ -n "$out" ] && [ -n "$url" ]
cp "$TEST_FIXTURES/${url##*/}" "$out"
EOF
chmod +x "$testdir/commands/uname" "$testdir/commands/curl"
export TEST_FIXTURES="$testdir/fixtures"
export HOME="$testdir/home with spaces"
export TMPDIR="$testdir/temp"
export PATH="$testdir/commands:$PATH"

for platform in 'Linux x86_64 linux amd64' 'Linux aarch64 linux arm64' 'Darwin amd64 darwin amd64' 'Darwin arm64 darwin arm64'; do
  set -- $platform
  export TEST_OS=$1 TEST_ARCH=$2
  asset="rexpresence-$3-$4"
  printf 'fixture for %s\n' "$asset" > "$TEST_FIXTURES/$asset"
  if command -v sha256sum >/dev/null 2>&1; then
    hash=$(sha256sum "$TEST_FIXTURES/$asset")
  else
    hash=$(shasum -a 256 "$TEST_FIXTURES/$asset")
  fi
  printf '%s  %s\n' "${hash%% *}" "$asset" > "$TEST_FIXTURES/SHA256SUMS"
  sh "$root/scripts/install.sh" > "$testdir/output"
  cmp "$TEST_FIXTURES/$asset" "$HOME/.local/bin/rexpresence"
  [ -x "$HOME/.local/bin/rexpresence" ]
  grep -F 'export PATH="$HOME/.local/bin:$PATH"' "$testdir/output" >/dev/null
  grep -F 'https://github.com/Luqueee/rexpresence#change-your-presence-from-rex' "$testdir/output" >/dev/null
done

printf 'existing installation\n' > "$HOME/.local/bin/rexpresence"
printf 'tampered download\n' > "$TEST_FIXTURES/$asset"
if sh "$root/scripts/install.sh" > "$testdir/output" 2>&1; then
  echo 'Tampered download was accepted' >&2; exit 1
fi
[ "$(cat "$HOME/.local/bin/rexpresence")" = 'existing installation' ]

# Duplicate entries are ambiguous even if one checksum is valid.
cat "$TEST_FIXTURES/SHA256SUMS" >> "$testdir/duplicate"
cat "$testdir/duplicate" >> "$TEST_FIXTURES/SHA256SUMS"
if sh "$root/scripts/install.sh" > "$testdir/output" 2>&1; then
  echo 'Duplicate checksum was accepted' >&2; exit 1
fi
[ "$(cat "$HOME/.local/bin/rexpresence")" = 'existing installation' ]

export TEST_OS=FreeBSD
if sh "$root/scripts/install.sh" > "$testdir/output" 2>&1; then
  echo 'Unsupported OS was accepted' >&2; exit 1
fi
[ "$(cat "$HOME/.local/bin/rexpresence")" = 'existing installation' ]
[ -z "$(ls -A "$TMPDIR")" ]
printf '%s\n' 'Unix installer smoke tests passed.'
