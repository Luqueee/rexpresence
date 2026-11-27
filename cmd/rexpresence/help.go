package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const helpText = `{rexpresence} - show your Rex activity on Discord

{Usage}
  rexpresence <command> [flags]

{Watcher}
  run [-d]              Start the watcher (-d runs it in the background)
  stop                  Stop the running watcher
  status                Show what the watcher is doing
  install               Start the watcher at login (macOS)
  uninstall             Remove the login service

{Presence}
  mode auto             Show the tool and group automatically
  mode custom TEXT      Show your own public text
  mode paused           Show nothing
  mode remaining        Print the minutes left on a timed pause
  pause [DURATION]      Hide your activity for a while (default 15m)
  resume                End a pause early
  group get ID          Tell whether a Rex session is shared
  group hide|show ID    Stop or allow sharing a Rex session
  card get FIELD        Print a card setting
  card set FIELD VALUE  Change a card setting
  card reset FIELD      Restore a card setting
  card preview FIELD    Preview a card setting without saving
                        (fields: line1, line2, logo, timer)

{Setup}
  setup [remove]        Add or remove the Rex command palette actions
  doctor                Check Rex, Discord and duplicate installs
  update                Install the latest release
  version               Print the version

{Flags} (run and install)
  -d                    Run in the background
  -session NAME         Pin a Rex session by name or ID
  -server HOST          Rex server label or address (same as rex -S)
  -project NAME         Public project alias shown on Discord
  -logo URL|KEY         Presence image: HTTPS URL or Discord art asset key
  -client-id ID         Override the Discord Application ID
`

// printHelp writes the command list; headings are bold on a color terminal.
func printHelp(w io.Writer) {
	bold := func(s string) string { return s }
	if f, ok := w.(*os.File); ok && os.Getenv("NO_COLOR") == "" {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			bold = func(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
		}
	}
	text := helpText
	for _, h := range []string{"rexpresence", "Usage", "Watcher", "Presence", "Setup", "Flags"} {
		text = strings.Replace(text, "{"+h+"}", bold(h), 1)
	}
	fmt.Fprint(w, text)
}
