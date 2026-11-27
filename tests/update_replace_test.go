package integration_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"rexpresence/internal/update"
)

func TestUpdateReplace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		newer    bool
		fetchErr error
		want     string
	}{
		{"no update", false, nil, "original"},
		{"failed verification", true, errors.New("checksum mismatch"), "original"},
		{"replacement", true, nil, "replacement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "rexpresence")
			if err := os.WriteFile(path, []byte("original"), 0751); err != nil {
				t.Fatal(err)
			}
			latest, newer, err := update.Replace(path, func(w io.Writer) (string, bool, error) {
				if _, err := io.WriteString(w, "replacement"); err != nil {
					t.Fatal(err)
				}
				return "v2", tc.newer, tc.fetchErr
			})
			if latest != "v2" || newer != (tc.newer && tc.fetchErr == nil) || !errors.Is(err, tc.fetchErr) {
				t.Fatalf("Replace = %q, %v, %v", latest, newer, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tc.want {
				t.Fatalf("executable = %q, %v", data, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0751 {
				t.Fatalf("mode = %v", info.Mode())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("unexpected temporary files: %v, %v", entries, err)
			}
		})
	}
}

func TestUpdateReplaceRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	for _, path := range []string{link, dir} {
		_, _, err := update.Replace(path, func(io.Writer) (string, bool, error) {
			t.Fatal("fetch called for nonregular target")
			return "", false, nil
		})
		if err == nil {
			t.Fatalf("accepted nonregular target %s", path)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("target = %q, %v", data, err)
	}
}

func TestUpdateReplaceRejectsChangedExecutable(t *testing.T) {
	for _, change := range []string{"identity", "metadata"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "rexpresence")
			if err := os.WriteFile(path, []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			_, _, err := update.Replace(path, func(w io.Writer) (string, bool, error) {
				if _, err := io.WriteString(w, "update"); err != nil {
					t.Fatal(err)
				}
				if change == "identity" {
					if err := os.Rename(path, filepath.Join(dir, "old")); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("different"), 0755); err != nil {
						t.Fatal(err)
					}
				} else {
					past := time.Now().Add(-time.Hour)
					if err := os.Chtimes(path, past, past); err != nil {
						t.Fatal(err)
					}
				}
				return "v2", true, nil
			})
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("error = %v", err)
			}
			want := "original"
			if change == "identity" {
				want = "different"
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("executable = %q, %v", data, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".rexpresence-update-") {
					t.Fatalf("temporary file left behind: %s", entry.Name())
				}
			}
		})
	}
}
