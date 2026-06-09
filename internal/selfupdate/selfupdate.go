// Package selfupdate checks for and installs newer published builds. The check
// compares the running binary's commit (from the build's embedded VCS info)
// against the latest commit on main, is cached for a day, and offline-safe. It
// sends nothing about the user; it is an anonymous read of public commit data.
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
	"runtime/debug"
	"strings"
	"time"
)

const (
	// DevVersion is the default version string of a locally built binary.
	DevVersion  = "0.1.0-dev"
	releaseBase = "https://github.com/neur0map/prowl-agent/releases/download/nightly"
	asset       = "prowl-agent-linux-amd64"
	commitsAPI  = "https://api.github.com/repos/neur0map/prowl-agent/commits/main"
	cacheTTL    = 24 * time.Hour
)

// Result reports update status. Checked is true once we determined up-to-date or
// not; Available is true when a newer build exists.
type Result struct {
	Available bool
	Checked   bool
	Current   string // short local commit
	Note      string // "offline", "unknown build", or empty
}

// Check reports whether main has advanced past the running binary's commit.
func Check(version string) Result {
	local := localCommit(version)
	if local == "" {
		return Result{Note: "unknown build"}
	}
	if c, ok := readCache(); ok {
		return Result{Available: c.Available, Checked: true, Current: shortSum(local)}
	}
	latest, err := latestCommit(2 * time.Second)
	if err != nil {
		return Result{Current: shortSum(local), Note: "offline"}
	}
	avail := !sameCommit(local, latest)
	writeCache(cache{CheckedAt: time.Now().Unix(), Available: avail})
	return Result{Available: avail, Checked: true, Current: shortSum(local)}
}

// localCommit returns the commit the running binary was built from, preferring
// the embedded VCS revision (set for any go build/install in the repo).
func localCommit(version string) string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	if version != "" && version != DevVersion {
		return version
	}
	return ""
}

// latestCommit reads the latest commit SHA on main from the public GitHub API.
func latestCommit(timeout time.Duration) (string, error) {
	body, err := download(commitsAPI, timeout)
	if err != nil {
		return "", err
	}
	var out struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.SHA == "" {
		return "", errors.New("no sha in response")
	}
	return out.SHA, nil
}

// sameCommit compares two revisions, tolerating short vs full SHAs.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < 7 {
		return a == b
	}
	return a[:n] == b[:n]
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
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "prowl-agent")
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
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
