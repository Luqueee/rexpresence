package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rexpresence/internal/update"
)

func TestDownloadRelease(t *testing.T) {
	const asset = "rexpresence-linux-amd64"
	payload := []byte("release binary")
	sum := sha256.Sum256(payload)
	valid := fmt.Sprintf("%x  %s\n", sum, asset)
	for _, tc := range []struct {
		name, current, tag, sums string
		wantNew, wantError       bool
		wantRequests             int
	}{
		{"same", "v1.2.3", "v1.2.3", valid, false, false, 2},
		{"no downgrade", "v2.0.0", "v1.2.3", valid, false, false, 2},
		{"numeric comparison", "v1.9.9", "v1.10.0", valid, true, false, 4},
		{"verified", "v1.2.2", "v1.2.3", valid, true, false, 4},
		{"checksum mismatch", "v1.2.2", "v1.2.3", strings.Repeat("0", 64) + "  " + asset + "\n", true, true, 4},
		{"duplicate", "v1.2.2", "v1.2.3", valid + valid, true, true, 3},
		{"malformed duplicate", "v1.2.2", "v1.2.3", valid + "broken  " + asset + "\n", true, true, 3},
		{"missing checksum", "v1.2.2", "v1.2.3", "", true, true, 3},
		{"prerelease", "v1.2.2", "v1.2.3-rc.1", valid, false, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				switch r.URL.Path {
				case "/owner/repo/releases/latest":
					http.Redirect(w, r, "/owner/repo/releases/tag/"+tc.tag, http.StatusFound)
				case "/owner/repo/releases/tag/" + tc.tag:
					fmt.Fprint(w, "release page")
				case "/owner/repo/releases/download/" + tc.tag + "/SHA256SUMS":
					fmt.Fprint(w, tc.sums)
				case "/owner/repo/releases/download/" + tc.tag + "/" + asset:
					w.Write(payload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var dst bytes.Buffer
			latest, newer, err := update.Download(context.Background(), server.Client(), server.URL+"/owner/repo/releases", tc.current, asset, &dst)
			if (err != nil) != tc.wantError || newer != tc.wantNew || latest != tc.tag {
				t.Fatalf("latest=%q newer=%v err=%v", latest, newer, err)
			}
			if len(paths) != tc.wantRequests {
				t.Fatalf("requests: %v", paths)
			}
			if !tc.wantNew && dst.Len() != 0 {
				t.Fatalf("no-op wrote %q", dst.String())
			}
			if tc.wantNew && !tc.wantError && !bytes.Equal(dst.Bytes(), payload) {
				t.Fatalf("download = %q", dst.String())
			}
		})
	}
}

func TestDownloadRejectsUntrustedRedirect(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("untrusted server was contacted")
	}))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/owner/repo/releases/tag/v2.0.0", http.StatusFound)
	}))
	defer server.Close()
	var dst bytes.Buffer
	_, _, err := update.Download(context.Background(), server.Client(), server.URL+"/owner/repo/releases", "v1.0.0", "binary", &dst)
	if err == nil || dst.Len() != 0 {
		t.Fatalf("err=%v, output=%q", err, dst.String())
	}
}

func TestValidVersion(t *testing.T) {
	for _, tag := range []string{"v0.0.0", "v1.2.3", "v999999999999999999999999.1.0"} {
		if !update.ValidVersion(tag) {
			t.Errorf("rejected %q", tag)
		}
	}
	for _, tag := range []string{"", "1.2.3", "v1.2", "v01.2.3", "v1.2.3-rc.1", "v1.2.3+build", "v1.2.-3"} {
		if update.ValidVersion(tag) {
			t.Errorf("accepted %q", tag)
		}
	}
}
