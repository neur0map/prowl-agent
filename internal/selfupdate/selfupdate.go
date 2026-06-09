// Package selfupdate checks for and installs newer published builds. The check
// is cheap (it fetches only a checksum), cached for a day, offline-safe, and a
// no-op for local dev builds. It sends nothing about the user; it is an
// anonymous download of a public checksum.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DevVersion is the default version of a locally built binary; update checks
	// are skipped for it so contributors are not nagged.
	DevVersion  = "0.1.0-dev"
	releaseBase = "https://github.com/neur0map/prowl-agent/releases/download/nightly"
	asset       = "prowl-agent-linux-amd64"
	cacheTTL    = 24 * time.Hour
)

// Result reports whether a newer published build is available.
type Result struct {
	Available bool
	Current   string
	Note      string // "dev build", "offline", or empty
}

// Check reports whether the published nightly binary differs from this one.
func Check(current string) Result {
	if current == "" || current == DevVersion {
		return Result{Current: current, Note: "dev build"}
	}
	if c, ok := readCache(); ok {
		return Result{Available: c.Available, Current: current}
	}
	remote, err := fetchChecksum(2 * time.Second)
	if err != nil {
		return Result{Current: current, Note: "offline"}
	}
	local, err := selfChecksum()
	if err != nil {
		return Result{Current: current, Note: "unknown"}
	}
	avail := !strings.EqualFold(remote, local)
	writeCache(cache{CheckedAt: time.Now().Unix(), Available: avail})
	return Result{Available: avail, Current: current}
}

// Apply downloads the latest published binary, verifies its checksum, and
// atomically replaces the running executable.
func Apply() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want, err := fetchChecksum(10 * time.Second)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}
	if cur, err := selfChecksum(); err == nil && strings.EqualFold(cur, want) {
		return "already on the latest build", nil
	}
	body, err := download(releaseBase+"/"+asset, 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
		return "", errors.New("checksum mismatch on downloaded binary")
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".prowl-agent-update-*")
	if err != nil {
		return "", fmt.Errorf("cannot write to %s (try a writable install dir or sudo): %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return "", fmt.Errorf("replace %s: %w", exe, err)
	}
	clearCache()
	return "updated to the latest build (" + shortSum(want) + ")", nil
}

// parseChecksum extracts the hex digest from a "sha256sum" line.
func parseChecksum(data []byte) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) < 32 {
		return "", errors.New("malformed checksum")
	}
	return fields[0], nil
}

func fetchChecksum(timeout time.Duration) (string, error) {
	body, err := download(releaseBase+"/"+asset+".sha256", timeout)
	if err != nil {
		return "", err
	}
	return parseChecksum(body)
}

func download(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func selfChecksum() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func shortSum(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

type cache struct {
	CheckedAt int64 `json:"checked_at"`
	Available bool  `json:"available"`
}

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "prowl-agent", "update.json")
}

func readCache() (cache, bool) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return cache{}, false
	}
	var c cache
	if json.Unmarshal(data, &c) != nil {
		return cache{}, false
	}
	if time.Since(time.Unix(c.CheckedAt, 0)) > cacheTTL {
		return cache{}, false
	}
	return c, true
}

func writeCache(c cache) {
	p := cachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if data, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}

func clearCache() { _ = os.Remove(cachePath()) }
