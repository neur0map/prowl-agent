package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

const (
	derivedIgnoreStart = "# prowl-agent: derived state"
	derivedIgnoreEnd   = "# /prowl-agent: derived state"
)

var derivedIgnorePatterns = []string{
	".prowl/index.db*",
	".prowl/cache/",
	".prowl/logs/",
	".prowl/editor/",
	".prowl/setup-applies.json",
	".prowl-setup.lock",
}

// EnsureIgnored makes sure pattern is present in root/.gitignore, creating the
// file if needed. It is idempotent.
func EnsureIgnored(root, pattern string) error {
	gi := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == pattern {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteString(pattern + "\n")
	return os.WriteFile(gi, buf.Bytes(), 0o644)
}

// EnsureDerivedIgnored ignores only rebuildable Prowl state. If a historical
// broad .prowl/ rule is present, it is preserved and followed by explicit
// negations for canonical settings, knowledge, and proposals. Unowned user
// lines are never removed or rewritten.
func EnsureDerivedIgnored(root string) error {
	gi := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	broad := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == ".prowl/" {
			broad = true
			break
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	lines := []string{derivedIgnoreStart}
	if broad {
		lines = append(lines,
			"!.prowl/",
			".prowl/*",
			"!.prowl/config.toml",
			"!.prowl/rules.toml",
			"!.prowl/knowledge/",
			"!.prowl/knowledge/**",
			"!.prowl/proposals/",
			"!.prowl/proposals/**",
		)
	}
	lines = append(lines, derivedIgnorePatterns...)
	lines = append(lines, derivedIgnoreEnd)
	block := []byte(strings.Join(lines, "\n") + "\n")

	start := bytes.Index(data, []byte(derivedIgnoreStart))
	if start >= 0 {
		endRel := bytes.Index(data[start:], []byte(derivedIgnoreEnd))
		if endRel >= 0 {
			end := start + endRel + len(derivedIgnoreEnd)
			if end < len(data) && data[end] == '\n' {
				end++
			}
			updated := append(append([]byte(nil), data[:start]...), block...)
			updated = append(updated, data[end:]...)
			if bytes.Equal(updated, data) {
				return nil
			}
			return os.WriteFile(gi, updated, 0o644)
		}
	}
	var out bytes.Buffer
	out.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		out.WriteByte('\n')
	}
	out.Write(block)
	return os.WriteFile(gi, out.Bytes(), 0o644)
}
