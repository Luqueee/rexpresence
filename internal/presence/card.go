package presence

import (
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Card contains the user's automatic presence card preferences.
type Card struct {
	Line1, Line2, Logo string
	Timer              bool
}

// DefaultCard returns the default automatic presence card.
func DefaultCard() Card {
	return Card{Line1: "{activity}", Line2: "Group: {group}", Logo: "{default}", Timer: true}
}

// ValidLogo reports whether s is a Discord asset key or an HTTPS URL.
func ValidLogo(s string) bool {
	if s == "" || len(s) > 4096 || !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	if strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
	}
	if s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func validCardTemplate(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 128 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	for len(s) > 0 {
		i := strings.IndexAny(s, "{}")
		if i < 0 {
			return true
		}
		if s[i] != '{' {
			return false
		}
		s = s[i:]
		matched := false
		for _, token := range [...]string{"{tool}", "{group}", "{project}", "{activity}"} {
			if strings.HasPrefix(s, token) {
				s = s[len(token):]
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func validCardField(field, value string) bool {
	switch field {
	case "line1", "line2":
		return validCardTemplate(value)
	case "logo":
		return value == "" || value == "{default}" || ValidLogo(value)
	case "timer":
		return value == "on" || value == "off"
	}
	return false
}

func cardField(card Card, field string) (string, error) {
	switch field {
	case "line1":
		return card.Line1, nil
	case "line2":
		return card.Line2, nil
	case "logo":
		return card.Logo, nil
	case "timer":
		if card.Timer {
			return "on", nil
		}
		return "off", nil
	default:
		return "", errors.New("invalid card field")
	}
}

// WithField returns a validated, unsaved edit. A nil value restores the default.
func (card Card) WithField(field string, value *string) (Card, error) {
	var v string
	if value == nil {
		var err error
		v, err = cardField(DefaultCard(), field)
		if err != nil {
			return Card{}, err
		}
	} else {
		v = *value
	}
	if !validCardField(field, v) {
		return Card{}, errors.New("invalid card field or value")
	}
	switch field {
	case "line1":
		card.Line1 = v
	case "line2":
		card.Line2 = v
	case "logo":
		card.Logo = v
	case "timer":
		card.Timer = v == "on"
	}
	return card, nil
}

func cardDirectory() (string, error) {
	path, err := modePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "card"), nil
}

// LoadCard reads all preferences, using defaults for unset fields.
func LoadCard() (Card, error) {
	card := DefaultCard()
	dir, err := cardDirectory()
	if err != nil {
		return Card{}, err
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		if err := privateDirectory(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return card, nil
			}
			return Card{}, err
		}
	}
	for _, field := range [...]string{"line1", "line2", "logo", "timer"} {
		path := filepath.Join(dir, field)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Card{}, err
		}
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			return Card{}, errors.New("invalid card settings file")
		}
		file, err := os.Open(path)
		if err != nil {
			return Card{}, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		value := string(data)
		if readErr != nil || closeErr != nil || len(data) > 4096 {
			return Card{}, errors.New("invalid card settings file")
		}
		card, err = card.WithField(field, &value)
		if err != nil {
			return Card{}, errors.New("invalid card settings file")
		}
	}
	return card, nil
}

// GetCardField reads one card preference.
func GetCardField(field string) (string, error) {
	if _, err := cardField(DefaultCard(), field); err != nil {
		return "", err
	}
	card, err := LoadCard()
	if err != nil {
		return "", err
	}
	return cardField(card, field)
}

// SetCardField atomically updates one card preference.
func SetCardField(field, value string) error {
	if !validCardField(field, value) {
		return errors.New("invalid card field or value")
	}
	dir, err := cardDirectory()
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := privateDirectory(path); err != nil {
			return err
		}
	}
	file, err := os.CreateTemp(dir, ".card-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(value)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, field))
}

// ResetCardField restores the default for one card preference.
func ResetCardField(field string) error {
	if _, err := cardField(DefaultCard(), field); err != nil {
		return err
	}
	dir, err := cardDirectory()
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		if err := privateDirectory(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	if err := os.Remove(filepath.Join(dir, field)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
