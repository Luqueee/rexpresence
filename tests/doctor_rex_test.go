package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rexpresence/internal/rex"
)

func TestRexPaletteStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture requires a Unix shell")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$#" != 3 ] || [ "$1" != "--autostart=false" ] || [ "$2" != "actions" ] || [ "$3" != "--json" ]; then exit 2; fi
if [ "${REX_SERVER+x}${REX_CLIENT+x}${REX_HOME+x}" != "" ]; then exit 3; fi
if [ "$DOCTOR_FAIL" = 1 ]; then echo 'private server details' >&2; exit 4; fi
printf '%s' "$DOCTOR_JSON"
`
	if err := os.WriteFile(filepath.Join(dir, "rex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("REX_SERVER", "remote.example:1234")
	t.Setenv("REX_HOME", "remote.example:1234")
	t.Setenv("REX_CLIENT", "remote-client")
	for _, tc := range []struct {
		name, output string
		want         int
		invalid      bool
	}{
		{"empty", `{"actions":[]}`, 0, false},
		{"one", `{"actions":[{"name":"discord_change_presence"},{"name":"discord_change_presence"}]}`, 1, false},
		{"both", `{"actions":[{"name":"discord_change_presence"},{"name":"discord_configure_card"},{"name":"other"}]}`, 2, false},
		{"invalid", `not json private`, 0, true},
		{"wrong shape", `{"actions":{}}`, 0, true},
		{"missing actions", `{}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DOCTOR_JSON", tc.output)
			got, err := rex.PaletteStatus(context.Background())
			if got != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("PaletteStatus() = %d, %v; want %d, invalid=%t", got, err, tc.want, tc.invalid)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("error exposed server output")
			}
		})
	}
	t.Setenv("DOCTOR_FAIL", "1")
	if _, err := rex.PaletteStatus(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("server failure error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rex.PaletteStatus(ctx); err == nil {
		t.Fatal("canceled context succeeded")
	}
	if runtime.GOOS != "darwin" {
		t.Setenv("PATH", t.TempDir())
		if _, err := rex.PaletteStatus(context.Background()); err == nil {
			t.Fatal("missing Rex CLI succeeded")
		}
	}
}
