package integration_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rexpresence/internal/setup"
)

func TestSetupDeclineAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	init := filepath.Join(dir, "init.lua")
	original := []byte("return {}\n")
	if err := os.WriteFile(init, original, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	called := false
	validate := func(path string) error {
		called = true
		if filepath.Dir(path) != dir {
			t.Errorf("validation path is outside config directory: %s", path)
		}
		if _, err := os.ReadFile(filepath.Join(dir, "rexpresence.lua")); err != nil {
			t.Error(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "require(\"rexpresence\")\nreturn {}\n" {
			t.Errorf("prospective init: %q, %v", data, err)
		}
		return nil
	}
	changed, err := setup.Install(init, []byte("module"), strings.NewReader("\n"), &out, validate)
	if err != nil || changed || called {
		t.Fatalf("decline: %v %v validation=%v", changed, err, called)
	}
	if !strings.Contains(out.String(), init) || !strings.Contains(out.String(), filepath.Join(dir, "rexpresence.lua")) {
		t.Fatal(out.String())
	}
	if _, err = os.Lstat(filepath.Join(dir, "rexpresence.lua")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("decline wrote module: %v", err)
	}
	out.Reset()
	changed, err = setup.Install(init, []byte("module"), strings.NewReader("yes\n"), &out, validate)
	if err != nil || !changed || !called {
		t.Fatalf("install: %v %v validation=%v", changed, err, called)
	}
	info, err := os.Stat(init)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	out.Reset()
	changed, err = setup.Install(init, []byte("module"), strings.NewReader(""), &out, func(string) error { t.Error("unexpected validation"); return nil })
	if err != nil || changed || !strings.Contains(strings.ToLower(out.String()), "already installed") {
		t.Fatalf("idempotency: %v %v %s", changed, err, out.String())
	}
}

func TestSetupCreatesMissingDirectoryOnlyAfterConsent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "missing", "config")
	init := filepath.Join(dir, "init.lua")
	var out bytes.Buffer
	changed, err := setup.Install(init, []byte("module"), strings.NewReader(""), &out, nil)
	if err != nil || changed {
		t.Fatalf("decline: %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("decline created directory: %v", err)
	}
	if !strings.Contains(out.String(), dir) {
		t.Fatalf("missing directory not shown: %s", out.String())
	}
	changed, err = setup.Install(init, []byte("module"), strings.NewReader("yes\n"), &out, nil)
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	for _, path := range []string{init, filepath.Join(dir, "rexpresence.lua")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatalf("%s has mode %v", path, info.Mode())
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("directory mode: %v %v", info, err)
		}
	}
}

func TestSetupRecognizesRequiresAndIgnoresComments(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"single quote", "local x = require 'rexpresence'\n", "local x = require 'rexpresence'\n"},
		{"long string", "require [[rexpresence]]\n", "require [[rexpresence]]\n"},
		{"parentheses with comment", "require -- comment\n ( \"rexpresence\" )\n", "require -- comment\n ( \"rexpresence\" )\n"},
		{"comments and strings", "-- require('rexpresence')\n--[=[require \"rexpresence\"]=]\nlocal x = \"require('rexpresence')\"\nreturn {}\n", "require(\"rexpresence\")\n-- require('rexpresence')\n--[=[require \"rexpresence\"]=]\nlocal x = \"require('rexpresence')\"\nreturn {}\n"},
		{"bom shebang crlf", "\xef\xbb\xbf#!/usr/bin/lua\r\nreturn {}\r\n", "\xef\xbb\xbf#!/usr/bin/lua\r\nrequire(\"rexpresence\")\r\nreturn {}\r\n"},
		{"field call", "other.require('rexpresence')\n", "require(\"rexpresence\")\nother.require('rexpresence')\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			init := filepath.Join(dir, "init.lua")
			if err := os.WriteFile(init, []byte(tc.input), 0644); err != nil {
				t.Fatal(err)
			}
			changed, err := setup.Install(init, []byte("module"), strings.NewReader("y\n"), &bytes.Buffer{}, nil)
			if err != nil || !changed {
				t.Fatalf("install: %v %v", changed, err)
			}
			data, err := os.ReadFile(init)
			if err != nil || string(data) != tc.want {
				t.Fatalf("init: %q, %v; want %q", data, err, tc.want)
			}
		})
	}
}

func TestSetupUpgradeAndValidationRollback(t *testing.T) {
	dir := t.TempDir()
	init := filepath.Join(dir, "init.lua")
	mod := filepath.Join(dir, "rexpresence.lua")
	if err := os.WriteFile(init, []byte("return {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod+".bak", []byte("previous backup"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	changed, err := setup.Install(init, []byte("new"), strings.NewReader("y\n"), &out, func(string) error { return errors.New("bad config") })
	if err == nil || changed {
		t.Fatalf("validation failure: %v %v", changed, err)
	}
	for path, want := range map[string]string{init: "return {}\n", mod: "old", mod + ".bak": "previous backup"} {
		data, e := os.ReadFile(path)
		if e != nil || string(data) != want {
			t.Fatalf("%s: %q %v", path, data, e)
		}
	}
	if _, err = os.Lstat(mod + ".bak.1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback left backup: %v", err)
	}
	if !strings.Contains(out.String(), mod+".bak.1") || !strings.Contains(out.String(), "Replace") {
		t.Fatal(out.String())
	}
	changed, err = setup.Install(init, []byte("new"), strings.NewReader("y\n"), &bytes.Buffer{}, nil)
	if err != nil || !changed {
		t.Fatalf("upgrade: %v %v", changed, err)
	}
	data, err := os.ReadFile(mod + ".bak.1")
	if err != nil || string(data) != "old" {
		t.Fatalf("backup: %q %v", data, err)
	}
}

func TestSetupRefusesUnsafeTargetsAndConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	init := filepath.Join(dir, "init.lua")
	mod := filepath.Join(dir, "rexpresence.lua")
	if err := os.WriteFile(init, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Install(mod, nil, strings.NewReader("y"), &bytes.Buffer{}, nil); err == nil {
		t.Fatal("accepted colliding paths")
	}
	if err := os.Symlink(init, mod); err == nil {
		if _, err = setup.Install(init, nil, strings.NewReader("y"), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("accepted symlink")
		}
		if err = os.Remove(mod); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := setup.Install(init, []byte("new"), strings.NewReader("y\n"), &bytes.Buffer{}, func(string) error {
		return os.WriteFile(init, []byte("user edit"), 0600)
	})
	if err == nil || changed {
		t.Fatalf("concurrent change: %v %v", changed, err)
	}
	data, err := os.ReadFile(init)
	if err != nil || string(data) != "user edit" {
		t.Fatalf("lost user change: %q %v", data, err)
	}
	if _, err = os.Lstat(mod); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("module not rolled back: %v", err)
	}
}
