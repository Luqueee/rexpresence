package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	palette "rexpresence/integrations/rex"
	"rexpresence/internal/autostart"
	"rexpresence/internal/discord"
	"rexpresence/internal/presence"
	"rexpresence/internal/rex"
	"rexpresence/internal/setup"
	"rexpresence/internal/status"
)

const defaultClientID = "1575562044294570255"

// Homebrew sets this to its stable opt path so the palette survives upgrades.
var paletteExecutable string

func main() {
	command := ""
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: rexpresence [flags] | setup | pause [DURATION] | resume | install [flags] | uninstall | status | mode auto|paused|remaining|custom TEXT | card get|reset FIELD | card set FIELD VALUE | card preview FIELD [VALUE]")
		flag.PrintDefaults()
	}
	switch command {
	case "setup":
		if len(args) != 0 {
			flag.Usage()
			os.Exit(2)
		}
		check(setupPalette())
		return
	case "pause":
		d := 15 * time.Minute
		if len(args) > 1 {
			flag.Usage()
			os.Exit(2)
		}
		if len(args) == 1 {
			var err error
			d, err = time.ParseDuration(args[0])
			check(err)
		}
		check(presence.Pause(d))
		fmt.Println("Presence paused for", d)
		return
	case "resume":
		if len(args) != 0 {
			flag.Usage()
			os.Exit(2)
		}
		check(presence.Resume())
		fmt.Println("Previous presence restored")
		return
	case "card":
		check(cardCommand(args))
		return
	case "mode":
		if len(args) == 0 || (args[0] == "custom" && len(args) != 2) || (args[0] != "custom" && len(args) != 1) {
			flag.Usage()
			os.Exit(2)
		}
		if args[0] == "remaining" {
			minutes, err := presence.PauseMinutes()
			check(err)
			fmt.Println(minutes)
			return
		}
		var text string
		if len(args) == 2 {
			text = args[1]
		}
		check(presence.SetMode(args[0], text))
		fmt.Println("Presence mode saved:", args[0])
		return
	case "status", "uninstall":
		if len(args) != 0 {
			flag.Usage()
			os.Exit(2)
		}
		if command == "status" {
			check(printStatus())
		} else {
			check(autostart.Uninstall())
			fmt.Println("Login service uninstalled.")
		}
		return
	case "", "install":
	default:
		flag.Usage()
		os.Exit(2)
	}
	clientID := flag.String("client-id", cmp.Or(os.Getenv("DISCORD_CLIENT_ID"), defaultClientID), "override the Discord Application ID (or set DISCORD_CLIENT_ID)")
	logo := flag.String("logo", "https://www.superlogical.com/rex-icon.png", "presence image: public HTTPS URL or Discord art asset key")
	server := flag.String("server", "", "Rex server label or address (same as rex -S)")
	session := flag.String("session", "", "pin a Rex session by name or ID (default on a local Mac: follow the selected session)")
	project := flag.String("project", "", "optional public project alias shown on Discord")
	flag.CommandLine.Parse(args)
	id, err := strconv.ParseUint(*clientID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != *clientID {
		fmt.Fprintln(os.Stderr, "Set -client-id or DISCORD_CLIENT_ID to your Discord application's numeric ID (not a bot token).")
		os.Exit(2)
	}
	if !presence.ValidLogo(*logo) {
		fmt.Fprintln(os.Stderr, "-logo must be a public HTTPS image URL or a Discord art asset key, not a local file path.")
		os.Exit(2)
	}
	if flag.NArg() != 0 || len(*project) > 64 || strings.IndexFunc(*project, unicode.IsControl) >= 0 {
		fmt.Fprintln(os.Stderr, "No positional arguments are accepted. -project must be at most 64 bytes without control characters.")
		os.Exit(2)
	}
	if command == "install" {
		var options []string
		if *clientID != defaultClientID {
			flag.Set("client-id", *clientID) // Preserve an environment override across logins.
		}
		flag.Visit(func(f *flag.Flag) {
			options = append(options, "-"+f.Name+"="+f.Value.String())
		})
		check(autostart.Install(options))
		fmt.Println("Login service installed. Run 'rexpresence status' to check it.")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, *clientID, *logo, *server, *session, *project)
	stop()
	check(err)
}

func setupPalette() error {
	path, validate, reload, err := rex.ConfigForSetup()
	if err != nil {
		return err
	}
	exe := paletteExecutable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return err
		}
	}
	changed, err := setup.Install(path, palette.Module(exe), os.Stdin, os.Stdout, validate)
	if err != nil || !changed {
		return err
	}
	if err := reload(); err != nil {
		return fmt.Errorf("palette files installed; open Rex locally and run 'rex config reload': %w", err)
	}
	fmt.Println("Palette ready. Open Rex's command palette and select Discord: Change Presence…")
	return nil
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "rexpresence:", err)
		os.Exit(1)
	}
}

func printStatus() error {
	if runtime.GOOS == "darwin" {
		installed, err := autostart.Installed()
		if err != nil {
			return err
		}
		if installed {
			fmt.Println("Start at login: installed")
		} else {
			fmt.Println("Start at login: not installed")
		}
	}
	return status.Print(os.Stdout)
}

func cardCommand(args []string) error {
	if (len(args) == 2 || len(args) == 3) && args[0] == "preview" {
		var value *string
		if len(args) == 3 {
			value = &args[2]
		}
		return previewCard(args[1], value)
	}
	if len(args) == 2 {
		switch args[0] {
		case "get":
			value, err := presence.GetCardField(args[1])
			if err == nil {
				fmt.Println(value)
			}
			return err
		case "reset":
			return presence.ResetCardField(args[1])
		}
	}
	if len(args) == 3 && args[0] == "set" {
		return presence.SetCardField(args[1], args[2])
	}
	return errors.New("use card get FIELD, card set FIELD VALUE, card reset FIELD, or card preview FIELD [VALUE] (line1, line2, logo, timer)")
}

func previewCard(field string, value *string) error {
	card, err := presence.LoadCard()
	if err != nil {
		return err
	}
	card, err = card.WithField(field, value)
	if err != nil {
		return err
	}
	s, err := status.ReadSnapshot()
	if err != nil {
		return err
	}
	var label string
	switch s.Mode {
	case "auto":
		label = "Automatic"
	case "paused":
		label = "Paused — nothing is shown"
	case "custom":
		label = "Custom — automatic lines are not shown"
	default:
		return errors.New("watcher is not ready to preview")
	}
	top, bottom, logo, timer := "Hidden", "Hidden", "Hidden", "Hidden"
	if p := s.Activity; p != nil && s.Mode != "paused" {
		p.Card = &card // ReadSnapshot owns this copy; the watcher is untouched.
		rendered := discord.Render(p, s.Logo)
		top = cmp.Or(rendered.Details, top)
		bottom = cmp.Or(rendered.State, bottom)
		if rendered.Assets != nil {
			logo = rendered.Assets.LargeImage
		}
		if rendered.Timestamps != nil {
			timer = "Shown"
		}
	} else if s.Mode != "paused" {
		label += " — no active pane"
	}
	_, err = fmt.Printf("%s\nTop line: %s\nBottom line: %s\nLogo: %s\nTimer: %s\n", label, top, bottom, logo, timer)
	return err
}

func run(ctx context.Context, clientID, logo, server, session, project string) error {
	live, err := status.Listen()
	if err != nil {
		return fmt.Errorf("open status endpoint: %w", err)
	}
	defer live.Close()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	raw := make(chan *presence.Activity)
	updates := make(chan *presence.Activity)
	done := make(chan error, 1)
	modeDone := make(chan error, 1)
	go func() {
		modeDone <- presence.ApplyMode(workerCtx, raw, updates, live.Mode, func(p *presence.Activity) { live.Activity(p, logo) })
		cancel()
	}()
	go func() {
		done <- discord.Publish(workerCtx, clientID, logo, updates, live.Discord)
		cancel()
	}()
	rexErr := rex.Watch(workerCtx, server, session, project, raw, live.Session)
	cancel()
	discordErr := <-done
	modeErr := <-modeDone
	if ctx.Err() != nil {
		return nil
	}
	if modeErr != nil && !errors.Is(modeErr, context.Canceled) {
		return modeErr
	}
	if discordErr != nil && !errors.Is(discordErr, context.Canceled) {
		return discordErr
	}
	return rexErr
}
