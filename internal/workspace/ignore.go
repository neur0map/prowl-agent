package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

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
			return nil // already ignored
		}
	}
	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteString(pattern + "\n")
	return os.WriteFile(gi, buf.Bytes(), 0o644)
}
