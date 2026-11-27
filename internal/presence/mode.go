package presence

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type modeState struct {
	Mode   string `json:"mode"`
	Text   string `json:"text,omitempty"`
	Resume string `json:"resume,omitempty"`
	Until  int64  `json:"until,omitempty"`
}

func modePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".rexpresence", "mode"), nil
}

func privateDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return errors.New("presence directory must be private")
	}
	return nil
}

func validText(text string) bool {
	if !utf8.ValidString(text) || strings.TrimSpace(text) != text {
		return false
	}
	n := 0
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
		n++
	}
	return n >= 2 && n <= 128
}

// SetMode atomically changes the per-user presence mode.
func SetMode(mode, text string) error {
	if mode != "auto" && mode != "custom" && mode != "paused" {
		return errors.New("invalid presence mode")
	}
	if mode == "custom" {
		if !utf8.ValidString(text) || strings.IndexFunc(text, unicode.IsControl) >= 0 {
			return errors.New("invalid custom presence text")
		}
		text = strings.TrimSpace(text)
		if !validText(text) {
			return errors.New("invalid custom presence text")
		}
	} else {
		text = ""
	}
	return saveMode(modeState{Mode: mode, Text: text})
}

// Pause hides the current presence for d, preserving its mode and custom text.
func Pause(d time.Duration) error {
	if d < time.Millisecond {
		return errors.New("pause duration must be at least 1ms (for example, 30m or 1h)")
	}
	prior, err := currentMode()
	if err != nil {
		return err
	}
	if prior.Mode == "paused" {
		return errors.New("already paused indefinitely")
	}
	resume := prior.Mode
	if resume == "pause-15m" {
		resume = prior.Resume
	}
	// Keep the existing on-disk discriminator so installed watchers honor the deadline.
	return saveMode(modeState{Mode: "pause-15m", Resume: resume, Text: prior.Text, Until: time.Now().Add(d).UnixMilli()})
}

func saveMode(state modeState) error {
	path, err := modePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if state.Mode != "auto" {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if err := privateDirectory(dir); err != nil {
		if state.Mode == "auto" && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if state.Mode == "auto" {
		err = os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mode-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func currentMode() (modeState, error) {
	path, err := modePath()
	if err != nil {
		return modeState{}, err
	}
	return readMode(path)
}

func (s modeState) minutesLeft() int64 {
	if s.Mode != "pause-15m" {
		return 0
	}
	ms := s.Until - time.Now().UnixMilli()
	if ms <= 0 {
		return 0
	}
	return 1 + (ms-1)/time.Minute.Milliseconds()
}

// PauseMinutes returns whole minutes rounded up, or zero outside a timed pause.
func PauseMinutes() (int64, error) {
	s, err := currentMode()
	return s.minutesLeft(), err
}

// Resume restores the mode saved by an active timed pause.
func Resume() error {
	s, err := currentMode()
	if err != nil {
		return err
	}
	if s.minutesLeft() == 0 {
		return errors.New("no active timed pause")
	}
	return SetMode(s.Resume, s.Text)
}

func readMode(path string) (modeState, error) {
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return modeState{Mode: "auto"}, nil
		}
		return modeState{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return modeState{Mode: "auto"}, nil
	}
	if err != nil {
		return modeState{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return modeState{}, errors.New("invalid presence mode file")
	}
	file, err := os.Open(path)
	if err != nil {
		return modeState{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 || !utf8.Valid(data) {
		return modeState{}, errors.New("invalid presence mode file")
	}
	var state modeState
	if json.Unmarshal(data, &state) != nil {
		return modeState{}, errors.New("invalid presence mode file")
	}
	base := state.Mode
	if base == "pause-15m" {
		if state.Until <= 0 || (state.Resume != "custom" && state.Resume != "auto") {
			return modeState{}, errors.New("invalid timed pause")
		}
		base = state.Resume
	} else if (base != "paused" && base != "custom") || state.Resume != "" || state.Until != 0 {
		return modeState{}, errors.New("invalid presence mode file")
	}
	if (base == "custom" && !validText(state.Text)) || (base != "custom" && state.Text != "") {
		return modeState{}, errors.New("invalid presence mode file")
	}
	return state, nil
}

// ApplyMode applies live per-user privacy settings to Rex activity updates.
func ApplyMode(ctx context.Context, input <-chan *Activity, output chan<- *Activity, report func(string), onActivity func(*Activity)) error {
	path, err := modePath()
	if err != nil {
		return err
	}
	var state modeState
	var card *Card
	var current, desired, sent, custom *Activity
	var reported string
	var sentOnce bool
	queued := true
	refresh := func() {
		next, err := readMode(path)
		if err == nil && next.Mode == "pause-15m" {
			// Expiry is read-only: an old timer can never overwrite a later manual choice.
			if time.Now().UnixMilli() >= next.Until {
				next.Mode = next.Resume
			} else {
				next = modeState{Mode: "paused"}
			}
		} else if err != nil {
			next = modeState{Mode: "paused"}
		}
		style, err := LoadCard()
		if err != nil {
			next = modeState{Mode: "paused"} // Never reveal fields hidden by an unreadable configuration.
		} else if style == DefaultCard() {
			card = nil
		} else if card == nil || style != *card {
			card = &style
		}
		if next.Mode == "custom" && (state.Mode != "custom" || state.Text != next.Text) {
			custom = &Activity{Custom: true, Details: next.Text, Started: time.Now().Unix()}
		}
		state = next
		if state.Mode != reported {
			reported = state.Mode
			if report != nil {
				report(reported)
			}
		}
	}
	equal := func(a, b *Activity) bool {
		return a == nil && b == nil || a != nil && b != nil && *a == *b
	}
	reconcile := func() {
		desired = nil
		if current != nil {
			switch state.Mode {
			case "auto":
				desired = current
			case "custom":
				desired = custom
			}
		}
		if desired != nil && card != nil {
			styled := *desired
			styled.Card = card
			if sent != nil && styled == *sent {
				desired = sent
			} else {
				desired = &styled
			}
		}
		queued = !sentOnce || !equal(desired, sent)
	}
	refresh()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var out chan<- *Activity
		if queued {
			out = output
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-input:
			if !ok {
				return nil
			}
			current = p
			reconcile()
		case <-ticker.C:
			refresh()
			reconcile()
		case out <- desired:
			if onActivity != nil {
				onActivity(desired)
			}
			sent = desired
			sentOnce = true
			queued = false
		}
	}
}
