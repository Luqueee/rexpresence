package rex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"rexpresence/internal/presence"
)

type sessionSnapshot struct {
	ID           string `json:"session_id"`
	Label        string `json:"label"`
	ActiveWindow string `json:"active_window_id"`
	Windows      []struct {
		ID           string `json:"window_id"`
		FocusedBlock string `json:"focused_block_id"`
	} `json:"windows"`
	Clients []struct {
		State string `json:"connection_state"`
		Info  struct {
			Kind string `json:"kind"`
		} `json:"info"`
	} `json:"clients"`
}

func (s sessionSnapshot) focused() string {
	active := false
	for _, c := range s.Clients {
		if c.Info.Kind != "cli" && (c.State == "" || c.State == "connected") {
			active = true
			break
		}
	}
	if active {
		for _, w := range s.Windows {
			if w.ID == s.ActiveWindow {
				return w.FocusedBlock
			}
		}
	}
	return ""
}

func label(name string) string {
	switch strings.ToLower(name) {
	case "nvim":
		return "Neovim"
	case "vim":
		return "Vim"
	case "vi":
		return "Vi"
	case "bash", "zsh", "fish", "sh", "dash", "nu", "pwsh":
		return "Shell"
	case "emacs":
		return "Emacs"
	case "helix", "hx":
		return "Helix"
	default:
		return "Terminal"
	}
}

type event struct {
	Type    string `json:"type"`
	Payload struct {
		SessionID string `json:"session_id"`
		Event     struct {
			Type    string `json:"type"`
			Payload struct {
				BlockID string `json:"block_id"`
				Creator string `json:"creator_name"`
				Name    string `json:"name"`
			} `json:"payload"`
		} `json:"event"`
	} `json:"payload"`
}

func (e event) relevant(session, block string) bool {
	if e.Type != "session_event" || e.Payload.SessionID != session {
		return false
	}
	switch e.Payload.Event.Type {
	case "active_window_changed":
		return true
	case "block_event":
		p := e.Payload.Event.Payload
		return p.BlockID == block && p.Creator == "com.superlogical.terminal" && (p.Name == "process_changed" || p.Name == "child_exited")
	}
	return false
}

type cli string

func findCLI() (cli, error) {
	path, err := exec.LookPath("rex")
	if err == nil || runtime.GOOS != "darwin" || !errors.Is(err, exec.ErrNotFound) {
		return cli(path), err
	}
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, dir := range dirs {
		for _, app := range []string{"Rex.app", "Rex Beta.app"} {
			path := filepath.Join(dir, app, "Contents/Helpers/Rex Server.rex/Contents/MacOS/rex")
			if _, err := exec.LookPath(path); err == nil {
				return cli(path), nil
			}
		}
	}
	return "", errors.New("Rex CLI not found in PATH or inside Rex.app or Rex Beta.app in /Applications or ~/Applications")
}

func (c cli) command(ctx context.Context, server string, args ...string) *exec.Cmd {
	if server != "" {
		args = append([]string{"-S", server}, args...)
	}
	return exec.CommandContext(ctx, string(c), args...)
}

func (c cli) inspect(ctx context.Context, server, session string) (sessionSnapshot, error) {
	args := []string{"session", "inspect"}
	if session != "" {
		args = append(args, session)
	}
	args = append(args, "--json")
	cmd := c.command(ctx, server, args...)
	out, err := cmd.Output()
	if err != nil {
		return sessionSnapshot{}, fmt.Errorf("cannot resolve Rex session; choose one with -session (see rex ls): %w", err)
	}
	var s sessionSnapshot
	if err := json.Unmarshal(out, &s); err != nil || s.ID == "" {
		return sessionSnapshot{}, errors.New("invalid rex session response")
	}
	return s, nil
}

func (c cli) process(ctx context.Context, server, session, block string) string {
	cmd := c.command(ctx, server, "block", "call", "-s", session, "-b", block, "process")
	out, err := cmd.Output()
	if err != nil {
		return "Terminal"
	}
	var p struct {
		Foreground *struct {
			Name string `json:"name"`
		} `json:"foreground"`
	}
	if json.Unmarshal(out, &p) != nil || p.Foreground == nil {
		return "Terminal"
	}
	return label(p.Foreground.Name)
}

func focusedSession(ctx context.Context) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print :Workspace.lastFocusedSession", filepath.Join(home, "Library/Preferences/com.superlogical.rex.plist")).Output()
	if err != nil {
		return ""
	}
	var focus struct {
		SessionID string `json:"sessionID"`
	}
	if json.Unmarshal(out, &focus) != nil {
		return ""
	}
	return focus.SessionID
}

// Watch publishes the focused Rex activity until the event stream ends or ctx is canceled.
func Watch(ctx context.Context, server, session, project string, updates chan<- *presence.Activity, reportSession func(string)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c, err := findCLI()
	if err != nil {
		return err
	}
	followFocus := runtime.GOOS == "darwin" && server == "" && session == ""
	var s sessionSnapshot
	var events <-chan event
	var done <-chan error
	if !followFocus {
		s, err = c.inspect(ctx, server, session)
		if err != nil {
			return err
		}
		cmd := c.command(ctx, server, "events", "-s", s.ID, "--json")
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("rex events: %w", err)
		}
		if err = cmd.Start(); err != nil {
			return fmt.Errorf("rex events: %w", err)
		}
		defer func() { cancel(); _ = cmd.Wait() }()
		eventStream := make(chan event)
		streamDone := make(chan error, 1)
		events, done = eventStream, streamDone
		go func() {
			scanner := bufio.NewScanner(pipe)
			scanner.Buffer(make([]byte, 4096), 4*1024*1024)
			for scanner.Scan() {
				var event event
				if json.Unmarshal(scanner.Bytes(), &event) != nil {
					continue
				}
				select {
				case eventStream <- event:
				case <-ctx.Done():
					return
				}
			}
			streamDone <- scanner.Err()
		}()
	}
	var current *presence.Activity
	queued := true
	block := ""
	refresh := func() error {
		id := s.ID
		if followFocus {
			id = focusedSession(ctx)
		}
		var snapshot sessionSnapshot
		var err error
		if id != "" {
			snapshot, err = c.inspect(ctx, server, id)
		}
		if followFocus && (id == "" || err != nil || snapshot.ID != id) {
			reportSession("")
			block = ""
			if current != nil {
				current = nil
				queued = true
			}
			return nil
		}
		if err != nil {
			return err
		}
		name := snapshot.Label
		if name == "" {
			name = snapshot.ID
		}
		reportSession(name)
		block = snapshot.focused()
		if block != "" {
			details := c.process(ctx, server, snapshot.ID, block)
			started := time.Now().Unix()
			if current != nil && current.Details == details {
				if current.SessionID == snapshot.ID && current.Group == snapshot.Label {
					return nil
				}
				started = current.Started
			}
			current = &presence.Activity{Details: details, Group: snapshot.Label, SessionID: snapshot.ID, Project: project, Started: started}
		} else {
			if current == nil {
				return nil
			}
			current = nil
		}
		queued = true
		return nil
	}
	if err := refresh(); err != nil {
		return err
	}
	// Focus and client changes also fire for CLI inspections. Reconcile them without a feedback loop.
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		var out chan<- *presence.Activity
		if queued {
			out = updates
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := refresh(); err != nil {
				return err
			}
		case out <- current:
			queued = false
		case err := <-done:
			if err != nil {
				return fmt.Errorf("rex events: %w", err)
			}
			return errors.New("rex event stream ended")
		case event := <-events:
			if event.Type == "session_destroyed" && event.Payload.SessionID == s.ID {
				return errors.New("rex session ended")
			}
			if event.relevant(s.ID, block) {
				if err := refresh(); err != nil {
					return err
				}
			}
		}
	}
}
