package integration_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rexpresence/internal/setup"
)

func TestSetupRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	init := filepath.Join(dir, "init.lua")
	module := filepath.Join(dir, "rexpresence.lua")
	original := "\xef\xbb\xbf#!/usr/bin/lua\r\nreturn {}\r\n"
	if err := os.WriteFile(init, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"old release", "new release"} {
		if changed, err := setup.Install(init, []byte(content), strings.NewReader("yes\n"), io.Discard, nil); err != nil || !changed {
			t.Fatalf("install: changed=%v, err=%v", changed, err)
		}
	}
	var output bytes.Buffer
	blocked := errors.New("invalid config")
	for _, attempt := range []struct {
		answer     string
		failure    error
		wantChange bool
	}{
		{answer: "\n"},
		{answer: "yes\n", failure: blocked},
		{answer: "yes\n", wantChange: true},
	} {
		output.Reset()
		validated := false
		validate := func(path string) error {
			validated = true
			data, err := os.ReadFile(path)
			if err != nil || string(data) != original {
				t.Fatalf("prospective config = %q, err=%v", data, err)
			}
			return attempt.failure
		}
		changed, err := setup.Remove(init, strings.NewReader(attempt.answer), &output, validate)
		if changed != attempt.wantChange || !errors.Is(err, attempt.failure) || validated != (attempt.answer == "yes\n") {
			t.Fatalf("remove: changed=%v, err=%v, validated=%v", changed, err, validated)
		}
		if !strings.Contains(output.String(), "Remove "+module) || !strings.Contains(output.String(), init) {
			t.Fatalf("incomplete preview: %q", output.String())
		}
		data, err := os.ReadFile(init)
		if err != nil || (string(data) == original) != attempt.wantChange {
			t.Fatalf("config = %q, err=%v", data, err)
		}
		_, err = os.Stat(module)
		if attempt.wantChange && !errors.Is(err, os.ErrNotExist) || !attempt.wantChange && err != nil {
			t.Fatalf("module stat: %v", err)
		}
	}
	if data, err := os.ReadFile(module + ".bak"); err != nil || !strings.HasSuffix(string(data), "old release") {
		t.Fatalf("backup = %q, err=%v", data, err)
	}
	if changed, err := setup.Remove(init, strings.NewReader("yes\n"), io.Discard, nil); err != nil || changed {
		t.Fatalf("second remove: changed=%v, err=%v", changed, err)
	}
}

func TestSetupRemovePreservesUnownedFiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		init    string
		install bool
		edit    bool
	}{
		{name: "legacy require", init: "require('rexpresence')\n", install: false},
		{name: "pre-existing module", install: true},
		{name: "edited module", install: true, edit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			init := filepath.Join(dir, "init.lua")
			module := filepath.Join(dir, "rexpresence.lua")
			if err := os.WriteFile(init, []byte(tc.init), 0600); err != nil {
				t.Fatal(err)
			}
			if !tc.edit {
				if err := os.WriteFile(module, []byte("old module"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.install {
				if changed, err := setup.Install(init, []byte("new module"), strings.NewReader("yes\n"), io.Discard, nil); err != nil || !changed {
					t.Fatalf("install: changed=%v, err=%v", changed, err)
				}
			}
			before, err := os.ReadFile(module)
			if err != nil {
				t.Fatal(err)
			}
			if tc.edit {
				before = append(before, []byte("\n-- local change\n")...)
				if err := os.WriteFile(module, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			changed, err := setup.Remove(init, strings.NewReader("yes\n"), &output, nil)
			if err != nil || changed != tc.install {
				t.Fatalf("remove: changed=%v, err=%v", changed, err)
			}
			if changed && !strings.Contains(output.String(), "Keep") || !changed && !strings.Contains(output.String(), "manual") {
				t.Fatalf("missing preservation notice: %q", output.String())
			}
			if data, err := os.ReadFile(module); err != nil || !bytes.Equal(data, before) {
				t.Fatalf("module changed: %q, %v", data, err)
			}
			if data, err := os.ReadFile(init); err != nil || string(data) != tc.init {
				t.Fatalf("config changed unexpectedly: %q, %v", data, err)
			}
		})
	}
}
