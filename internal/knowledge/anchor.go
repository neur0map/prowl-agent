package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AnchorStatus describes whether deterministic source evidence still matches.
type AnchorStatus string

const (
	AnchorCurrent AnchorStatus = "current"
	AnchorStale   AnchorStatus = "stale"
	AnchorMissing AnchorStatus = "missing"
	AnchorInvalid AnchorStatus = "invalid"
)

// AnchorCheck is the inspectable result of checking one source anchor.
type AnchorCheck struct {
	Anchor   Anchor       `json:"anchor"`
	Status   AnchorStatus `json:"status"`
	Expected string       `json:"expected,omitempty"`
	Actual   string       `json:"actual,omitempty"`
	Message  string       `json:"message,omitempty"`
}

// HashRegion returns a SHA-256 hash for a 1-based inclusive line range. CRLF
// and LF are normalized to LF; a final newline does not affect the digest.
func HashRegion(data []byte, lineStart, lineEnd int) (string, error) {
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	normalized = bytes.TrimSuffix(normalized, []byte("\n"))
	lines := bytes.Split(normalized, []byte("\n"))
	if lineStart < 1 || lineEnd < lineStart || lineEnd > len(lines) {
		return "", fmt.Errorf("line range %d-%d is outside 1-%d", lineStart, lineEnd, len(lines))
	}
	region := bytes.Join(lines[lineStart-1:lineEnd], []byte("\n"))
	sum := sha256.Sum256(region)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CheckAnchor checks a source anchor relative to sourceRoot without mutating it.
func CheckAnchor(sourceRoot string, anchor Anchor) AnchorCheck {
	result := AnchorCheck{Anchor: anchor, Expected: anchor.ContentHash}
	clean := filepath.Clean(filepath.FromSlash(anchor.Path))
	if anchor.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(filepath.ToSlash(clean), "../") {
		result.Status = AnchorInvalid
		result.Message = "unsafe or empty source path"
		return result
	}
	data, err := os.ReadFile(filepath.Join(sourceRoot, clean))
	if os.IsNotExist(err) {
		result.Status = AnchorMissing
		result.Message = "source file is missing"
		return result
	}
	if err != nil {
		result.Status = AnchorInvalid
		result.Message = err.Error()
		return result
	}
	actual, err := HashRegion(data, anchor.LineStart, anchor.LineEnd)
	if err != nil {
		result.Status = AnchorInvalid
		result.Message = err.Error()
		return result
	}
	result.Actual = actual
	if actual == anchor.ContentHash {
		result.Status = AnchorCurrent
	} else {
		result.Status = AnchorStale
		result.Message = "anchored source region changed"
	}
	return result
}
