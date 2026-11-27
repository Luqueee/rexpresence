# Contributing to rexpresence

For a bug report, include the OS and architecture, Rex and rexpresence versions, the action you selected, and what you expected to see. Redact session labels, custom presence text, project aliases, and personal paths. Do not post tokens or private logs in an issue.

## Local checks

Install Go (1.23 minimum) and Lua (`lua` must be on `PATH`). From the project root on Linux or macOS:

```sh
gofmt -l .                    # should print nothing
go vet ./...
TMPDIR=/tmp go test -race -count=1 ./tests
```

Keep black-box tests in `tests/`, separate from the implementation. CI runs them on Linux and macOS; the macOS job also exercises session following and LaunchAgent behavior. The tests use local fixtures, not your Discord profile.

Run `sh tests/install_unix_test.sh` on Linux or macOS and `.\tests\install_windows_test.ps1` in Windows PowerShell. These installer tests use local download fixtures and never launch Discord or Rex.

## Releases (maintainers)

Merge the changes you want to release into `main` and wait for CI. To publish, push a new `vMAJOR.MINOR.PATCH` tag at the tested commit. No tag is needed for a normal pull request or push. The release workflow reruns CI on the tag, cross-compiles the standalone binaries, and publishes them with the Rex Lua action, license notices, and `SHA256SUMS`. It does not deploy anything to users' machines. Do not reuse a published tag.

After publishing a stable tag, update the source URL and SHA-256 in the [Homebrew tap](https://github.com/Luqueee/homebrew-tap/blob/main/Formula/rexpresence.rb). Pass `-X main.releaseVersion=v#{version}` in the formula's Go linker flags alongside `main.paletteExecutable`; `rexpresence version` should report the tag. Its macOS CI installs the formula and runs `brew test`.

To build and verify the same assets locally without publishing, run on Linux with Bash, Go, and GNU coreutils:

```sh
bash scripts/release.sh v0.4.0   # use the release tag you want to check
(cd dist && sha256sum -c SHA256SUMS)
```
