package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rexpresence/internal/presence"
)

func TestCardSettingsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	defaults := presence.Card{Line1: "{activity}", Line2: "Group: {group}", Logo: "{default}", Timer: true}
	if got, err := presence.LoadCard(); err != nil || got != defaults || presence.DefaultCard() != defaults {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	values := map[string]string{"line1": " {tool} / {project} ", "line2": "", "logo": "", "timer": "off"}
	for field, value := range values {
		if err := presence.SetCardField(field, value); err != nil {
			t.Fatal(err)
		}
	}
	want := presence.Card{Line1: " {tool} / {project} ", Line2: "", Logo: "", Timer: false}
	if got, err := presence.LoadCard(); err != nil || got != want {
		t.Fatalf("saved: %+v, %v", got, err)
	}
	for field, value := range values {
		if got, err := presence.GetCardField(field); err != nil || got != value {
			t.Fatalf("get %s: %q, %v", field, got, err)
		}
		info, err := os.Stat(filepath.Join(home, ".rexpresence", "card", field))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("file permissions: %v, %v", info, err)
		}
	}
	for field := range values {
		if err := presence.ResetCardField(field); err != nil {
			t.Fatal(err)
		}
		if err := presence.ResetCardField(field); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := presence.LoadCard(); err != nil || got != defaults {
		t.Fatalf("reset: %+v, %v", got, err)
	}
}

func TestCardSettingsValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct{ field, value string }{
		{"line1", "{unknown}"}, {"line1", "{tool"}, {"line1", "tool}"},
		{"line2", "{{group}}"}, {"line2", "secret\nvalue"}, {"line1", strings.Repeat("é", 129)},
		{"line1", "\xff"}, {"logo", "../secret"}, {"logo", "/tmp/image"},
		{"logo", "https://user:secret@example.com/image"}, {"logo", "http://example.com/image"},
		{"timer", "true"}, {"unknown", "secret-value"},
	} {
		err := presence.SetCardField(tc.field, tc.value)
		if err == nil || strings.Contains(err.Error(), tc.value) {
			t.Errorf("accepted or exposed invalid %s: %v", tc.field, err)
		}
	}
	for _, value := range []string{"asset_key-123", "https://example.com/image.png", "{default}", ""} {
		if err := presence.SetCardField("logo", value); err != nil {
			t.Errorf("valid logo %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "{default}", "../logo", "https://user:pass@example.com/", "http://example.com/"} {
		if presence.ValidLogo(value) {
			t.Errorf("invalid standalone logo accepted: %q", value)
		}
	}
	for _, value := range []string{"rex-icon", "https://example.com/logo.png"} {
		if !presence.ValidLogo(value) {
			t.Errorf("valid standalone logo rejected: %q", value)
		}
	}
	if _, err := presence.GetCardField("../logo"); err == nil {
		t.Error("accepted invalid get field")
	}
	if err := presence.ResetCardField("../logo"); err == nil {
		t.Error("accepted invalid reset field")
	}
}

func TestCardSettingsCorruptionFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := presence.SetCardField("line1", "safe"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".rexpresence", "card", "line1")
	for _, value := range []string{"{bad}", strings.Repeat("x", 4097), "\xff"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := presence.LoadCard(); err == nil {
			t.Errorf("accepted corrupt settings %q", value[:1])
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := presence.LoadCard(); err == nil {
		t.Error("accepted symlink settings")
	}
}
