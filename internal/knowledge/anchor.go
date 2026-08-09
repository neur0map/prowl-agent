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

// SymbolResolver maps a source anchor's (path, data, symbol) to the symbol's
// current 1-based inclusive line range. A symbol-based anchor is re-resolved on
// every check, so it tracks the symbol across line shifts. A nil resolver or
// ok=false falls back to the anchor's explicit line range.
type SymbolResolver func(path string, data []byte, symbol string) (lineStart, lineEnd int, ok bool)

// anchorRegion returns the line range an anchor pins in data: a symbol anchor
// re-resolves via resolve; otherwise the explicit line range is used.
func anchorRegion(a Anchor, data []byte, resolve SymbolResolver) (int, int, bool) {
	if a.Symbol != "" && resolve != nil {
		return resolve(a.Path, data, a.Symbol)
	}
	if a.LineStart >= 1 && a.LineEnd >= a.LineStart {
		return a.LineStart, a.LineEnd, true
	}
	return 0, 0, false
}

// FillMissingAnchorHashes computes content_hash for any anchor that omits it,
// reading the current source under sourceRoot. An anchor may pin its region by
// symbol name (resolved via resolve, tracking the symbol across line shifts) or
// by explicit line range. Authoring knowledge is about the code as it stands
// now, so the hash is computed from the current region; drift is detected later.
// This removes the need to hash the region by hand -- the friction that
// otherwise leaves anchors untracked and permanently reported stale. An
// unreadable path or unresolved region is left empty for lint to report.
func FillMissingAnchorHashes(doc *Document, sourceRoot string, resolve SymbolResolver) {
	if doc == nil {
		return
	}
	for i := range doc.Prowl.Anchors {
		a := &doc.Prowl.Anchors[i]
		if a.ContentHash != "" || a.Path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(a.Path)))
		if err != nil {
			continue
		}
		start, end, ok := anchorRegion(*a, data, resolve)
		if !ok {
			continue
		}
		if hash, err := HashRegion(data, start, end); err == nil {
			a.ContentHash = hash
		}
	}
}

// CheckAnchor checks a source anchor with no symbol resolution (explicit line
// ranges only). Prefer CheckAnchorResolved when symbol anchors are in use.
func CheckAnchor(sourceRoot string, anchor Anchor) AnchorCheck {
	return CheckAnchorResolved(sourceRoot, anchor, nil)
}

// CheckAnchorResolved checks a source anchor without mutating it. A symbol-based
// anchor re-resolves its line range via resolve, so it stays current across line
// shifts and stales only when the symbol's body changes.
func CheckAnchorResolved(sourceRoot string, anchor Anchor, resolve SymbolResolver) AnchorCheck {
	result := AnchorCheck{Anchor: anchor, Expected: anchor.ContentHash}
	clean := filepath.Clean(filepath.FromSlash(anchor.Path))
	if anchor.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(filepath.ToSlash(clean), "../") {
		result.Status = AnchorInvalid
		result.Message = "unsafe or empty source path"
		return result
	}
	data, err := readRootFile(sourceRoot, filepath.ToSlash(clean), MaxDocumentBytes)
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
	start, end, ok := anchorRegion(anchor, data, resolve)
	if !ok {
		if anchor.Symbol != "" {
			result.Status = AnchorMissing
			result.Message = "symbol not found: " + anchor.Symbol
			return result
		}
		result.Status = AnchorInvalid
		result.Message = "anchor has neither a resolvable symbol nor a line range"
		return result
	}
	actual, err := HashRegion(data, start, end)
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
