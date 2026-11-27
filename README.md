# rexpresence

[![CI](https://github.com/Luqueee/rexpresence/actions/workflows/ci.yml/badge.svg)](https://github.com/Luqueee/rexpresence/actions/workflows/ci.yml)

Show your foreground tool and [Rex](https://www.superlogical.com/rex) group on Discord, change the card from Rex's command palette, or pause sharing. The group is the label of the Rex session containing the focused pane, not the terminal title. This is an independent integration, not an official Rex or Discord product.

## Quick start

1. Open Rex and the Discord desktop app. On macOS, `rexpresence` uses `rex` from `PATH` if available. Otherwise, it looks inside **Rex.app** and **Rex Beta.app** in `/Applications` and `~/Applications`. On other systems, `rex` must be on `PATH`.
2. Build with Go 1.23 or later and run:

   ```sh
   go build -o rexpresence ./cmd/rexpresence
   ./rexpresence
   ```

Once a version is tagged, [GitHub Releases](https://github.com/Luqueee/rexpresence/releases) provides binaries for macOS and Linux (ARM64 and x86-64), and Windows (x86-64). Download the binary for your OS and architecture and compare its SHA-256 with `SHA256SUMS` from the same release. On an Apple Silicon Mac, for example:

```sh
shasum -a 256 rexpresence-darwin-arm64
mkdir -p ~/.local/bin
install -m 755 rexpresence-darwin-arm64 ~/.local/bin/rexpresence
~/.local/bin/rexpresence
```

On a local Mac, run without arguments to follow the session selected in Rex. Pass `-session NAME` to pin a session instead. For a remote server, use `-server HOST` with the same host you would pass to `rex -S HOST ls`; specify `-session` if Rex cannot choose between sessions.

The binary includes a public Discord Application ID. To use your own application, pass `-client-id` or set `DISCORD_CLIENT_ID`. The command-line flag takes precedence.

The presence uses the [official Rex icon](https://www.superlogical.com/rex-icon.png) by default; no upload is needed. To change it, pass `-logo` with a public HTTPS image URL, or the key of an image already uploaded to your Discord application's **Rich Presence → Art Assets** (for example, `rexpresence -logo team-logo`). Local files are not uploaded.

By default the **session label is public** in automatic mode (for example, `Shell` and `Group: polished-sierra`). No path, terminal title, command arguments, or repository name is extracted. Use `-project "My project"` to supply a public project alias.

Local Mac selection is checked every three seconds. If Discord restarts, the current activity is restored. Press `Ctrl+C` to stop a foreground run.

## Change your presence from Rex

Open the **local Mac session** in Rex, open its command palette and choose **Discord: Change Presence…**. **Automatic** shows the tool and the session label. **Custom public text** shows only what you enter, without the group. **Pause for 15 minutes** hides the activity and then restores the previous mode, including any custom text. Choosing it again starts a fresh 15 minutes. Changing mode manually cancels the deadline. **Pause indefinitely** stays paused until you explicitly choose a public mode. Canceling the picker changes nothing.

During a timed pause, reopening the menu shows the minutes left (rounded up). **Resume now** restores the previous mode immediately without asking for your custom text again. Neither the countdown nor this option appears during an indefinite pause.

The same controls are available in the terminal:

```sh
rexpresence mode custom "Reviewing a pull request"
rexpresence mode pause-15m
rexpresence mode remaining  # minutes left; 0 when there is no active timed pause
rexpresence mode resume     # restore the saved automatic or custom mode now
rexpresence mode paused
rexpresence mode auto
```

The setting is private to your OS account, applies to all its watchers, and survives restarts. Changes take effect within about a second. Custom text is public on Discord while a pane is active. `rexpresence status` shows the applied mode without printing that text.

**Edit the card:** press **Cmd+Shift+P → Discord: Configure Card…**. To show just your Rex group, choose **Bottom line → Rex group name**. Read the result under **Apply change**, then select it to save. **Back** lets you try another option; **Esc** cancels. The preview shows the selected field in the watcher's current mode; it sends nothing to Discord. Changes take effect within about a second after applying, persist across logins, and do not switch modes. **Hide this line** omits a line; **Restore default** resets only the selected field. Discord controls the application name and may display a placeholder when no image is sent.

Choose **Custom text or template…** to enter your own wording. Use literal text or placeholders: `{tool}` (foreground tool), `{group}` (session label), `{project}` (explicit project alias), or `{activity}` (tool plus alias if set). For example, `In {group}` adds a prefix. A line that depends on an unavailable value is omitted. Automatic templates never expand in custom mode; logo and timer preferences apply to both modes.

The equivalent CLI is `rexpresence card get FIELD`, `rexpresence card set FIELD VALUE`, or `rexpresence card reset FIELD`, with fields `line1`, `line2`, `logo`, and `timer`. The timer accepts `on` or `off`. The logo accepts an HTTPS image URL, a Discord asset key, an empty value, or `{default}` to use the startup `-logo`.

For a read-only preview in the terminal, use `rexpresence card preview line2 '{group}'`. An empty value previews hiding the field; omitting the value previews a reset. Preview uses the watcher's current mode and requires one running watcher. It does not need Discord to be connected.

**Palette setup:** keep the binary at `~/.local/bin/rexpresence`. Copy [`integrations/rex/rexpresence.lua`](integrations/rex/rexpresence.lua) next to your local Rex `init.lua`, add `require("rexpresence")` to that file, and run `rex config reload`. This action runs on the Rex server hosting the session; installing it on a remote host does not control the Mac.

## Run at login on macOS

Keep the binary in a permanent location such as `~/.local/bin/rexpresence`. Stop any foreground copy with `Ctrl+C`, then run:

```sh
~/.local/bin/rexpresence install    # start now and at login
~/.local/bin/rexpresence status     # inspect the watcher without changing your presence
~/.local/bin/rexpresence uninstall  # stop the login service and remove it
```

`install` accepts the same flags as a foreground run (e.g. `install -session work`). No `sudo` is needed. `status` distinguishes installation from a running watcher, names the session Rex actually selected, and reports whether Discord acknowledged the latest activity. An acknowledgement is not proof that other Discord users can see your profile.

## Development

`cmd/rexpresence` contains the CLI. `internal/rex` watches the focused pane; `internal/discord` manages the IPC connection. Run the black-box tests in `tests/` with `go test ./tests`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for local checks, bug reports, and the tag-based release process.

## License and attribution

[BSD-4-Clause](LICENSE). If you advertise features or use of redistributed rexpresence software, include this acknowledgement: **This product includes software developed by the rexpresence contributors.** The license does not require you to advertise. The Rex icon is referenced from its official URL; this project's license does not grant rights to the icon or third-party trademarks.
