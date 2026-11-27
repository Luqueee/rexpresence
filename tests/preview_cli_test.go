package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rexpresence/internal/presence"
	"rexpresence/internal/status"
)

func TestCardPreviewCLI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "rexpresence")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/rexpresence").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	home, err := os.MkdirTemp("", "rp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	live, err := status.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { live.Close() })
	live.Mode("auto")
	live.Activity(&presence.Activity{Details: "Vim", Group: "work", Project: "demo"}, "team-logo")
	if err := presence.SetCardField("line2", "saved"); err != nil {
		t.Fatal(err)
	}
	preview := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(bin, append([]string{"card", "preview"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("preview: %v: %s", err, out)
		}
		return string(out)
	}
	if got := preview("line2", "In {group}"); got != "Automatic\nTop line: Vim · demo\nBottom line: In work\nLogo: team-logo\nTimer: Shown\n" {
		t.Fatal(got)
	}
	if got := preview("line2", ""); !strings.Contains(got, "Bottom line: Hidden\n") {
		t.Fatal(got)
	}
	if got := preview("line2"); !strings.Contains(got, "Bottom line: Group: work\n") {
		t.Fatal(got)
	}
	if saved, err := presence.GetCardField("line2"); err != nil || saved != "saved" {
		t.Fatalf("preview modified saved field: %q, %v", saved, err)
	}
	live.Mode("custom")
	live.Activity(&presence.Activity{Custom: true, Details: "Literal {group}"}, "team-logo")
	if got := preview("line2", "private"); !strings.Contains(got, "Top line: Literal {group}\nBottom line: Hidden\n") || strings.Contains(got, "private") {
		t.Fatal(got)
	}
	live.Mode("paused")
	if got := preview("logo", "team-logo"); strings.Contains(got, "Literal") || strings.Contains(got, "team-logo") || !strings.HasPrefix(got, "Paused") {
		t.Fatal(got)
	}
	live.Mode("private")
	live.Activity(&presence.Activity{Details: "Sensitive tool", Group: "Sensitive group"}, "private-logo")
	if got, want := preview("line2"), "Private group — nothing is shown\nTop line: Hidden\nBottom line: Hidden\nLogo: Hidden\nTimer: Hidden\n"; got != want {
		t.Fatalf("private preview: %q", got)
	}
}
