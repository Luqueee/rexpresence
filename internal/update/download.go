package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	maxChecksums = 1 << 20
	maxAsset     = 256 << 20
)

// ValidVersion reports whether tag is a stable semantic version tag.
func ValidVersion(tag string) bool {
	if !strings.HasPrefix(tag, "v") {
		return false
	}
	parts := strings.Split(tag[1:], ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func newerVersion(latest, current string) bool {
	a, b := strings.Split(latest[1:], "."), strings.Split(current[1:], ".")
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Download writes the verified release asset to dst when a newer release exists.
// The caller must discard dst on error, as bytes are written before verification.
func Download(ctx context.Context, client *http.Client, origin string, current, asset string, dst io.Writer) (latest string, newer bool, err error) {
	if !ValidVersion(current) {
		return "", false, fmt.Errorf("invalid current version %q", current)
	}
	if asset == "" || asset == "." || asset == ".." || strings.ContainsAny(asset, "/\\ \t\r\n") || dst == nil {
		return "", false, errors.New("invalid asset or destination")
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || !strings.HasSuffix(base.Path, "/releases") {
		return "", false, errors.New("invalid HTTPS releases origin")
	}
	live := strings.EqualFold(base.Hostname(), "github.com")
	if live && base.Host != "github.com" {
		return "", false, errors.New("invalid GitHub origin host")
	}
	allowed := func(u *url.URL) bool {
		if u.Scheme != "https" || u.User != nil {
			return false
		}
		if !live {
			return u.Host == base.Host
		}
		switch u.Host {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return true
		}
		return false
	}
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !allowed(req.URL) {
			return errors.New("release redirect to untrusted host")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many release redirects")
		}
		return nil
	}
	get := func(address string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		resp, err := copyClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: %s", address, resp.Status)
		}
		return resp, nil
	}
	resp, err := get(origin + "/latest")
	if err != nil {
		return "", false, err
	}
	final := resp.Request.URL
	closeErr := resp.Body.Close()
	if closeErr != nil {
		return "", false, closeErr
	}
	prefix := base.Path + "/tag/"
	if final.Host != base.Host || !strings.HasPrefix(final.Path, prefix) || final.RawQuery != "" || final.Fragment != "" {
		return "", false, errors.New("latest release did not resolve to a canonical tag")
	}
	latest = strings.TrimPrefix(final.Path, prefix)
	if !ValidVersion(latest) {
		return "", false, fmt.Errorf("invalid latest version %q", latest)
	}
	if !newerVersion(latest, current) {
		return latest, false, nil
	}
	newer = true
	pinned := origin + "/download/" + latest + "/"
	resp, err = get(pinned + "SHA256SUMS")
	if err != nil {
		return latest, true, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxChecksums+1))
	closeErr = resp.Body.Close()
	if readErr != nil {
		return latest, true, readErr
	}
	if closeErr != nil {
		return latest, true, closeErr
	}
	if len(data) > maxChecksums {
		return latest, true, errors.New("checksum file too large")
	}
	var expected []byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		matched := false
		for _, field := range fields {
			if strings.TrimPrefix(field, "*") == asset {
				matched = true
			}
		}
		if !matched {
			continue
		}
		if expected != nil || len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset || len(fields[0]) != 64 {
			return latest, true, errors.New("invalid or duplicate asset checksum")
		}
		expected, err = hex.DecodeString(fields[0])
		if err != nil {
			return latest, true, errors.New("invalid asset checksum")
		}
	}
	if err := scanner.Err(); err != nil {
		return latest, true, err
	}
	if expected == nil {
		return latest, true, errors.New("asset checksum not found")
	}
	resp, err = get(pinned + url.PathEscape(asset))
	if err != nil {
		return latest, true, err
	}
	hash := sha256.New()
	n, readErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(resp.Body, maxAsset+1))
	closeErr = resp.Body.Close()
	if readErr != nil {
		return latest, true, readErr
	}
	if closeErr != nil {
		return latest, true, closeErr
	}
	if n > maxAsset {
		return latest, true, errors.New("release asset too large")
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return latest, true, errors.New("release asset checksum mismatch")
	}
	return latest, true, nil
}
