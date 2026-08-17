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
	// AnchorMoved means the anchored lines are byte-identical but now sit at a
	// different line range: an edit above them shifted the region rather than
	// changing it. The evidence still holds, so the note stays trustworthy and
	// only its coordinates need repair.
	AnchorMoved AnchorStatus = "moved"
)

// AnchorCheck is the inspectable result of checking one source anchor.
type AnchorCheck struct {
	Anchor   Anchor       `json:"anchor"`
	Status   AnchorStatus `json:"status"`
	Expected string       `json:"expected,omitempty"`
	Actual   string       `json:"actual,omitempty"`
	Message  string       `json:"message,omitempty"`
	// MovedStart and MovedEnd carry the relocated 1-based inclusive line range
	// when Status is AnchorMoved. They are the range callers should cite and
	// write back; the anchor's own range is the stale one.
	MovedStart int `json:"moved_start,omitempty"`
	MovedEnd   int `json:"moved_end,omitempty"`
}

// Region reports the line range a check believes is authoritative now: the
// relocated range for a moved anchor, otherwise the anchor's own range.
func (c AnchorCheck) Region() (int, int) {
	if c.Status == AnchorMoved && c.MovedStart > 0 {
		return c.MovedStart, c.MovedEnd
	}
	return c.Anchor.LineStart, c.Anchor.LineEnd
}

// lineIndex is normalized source addressed by line without copying any line: it
// keeps one offset per line start, so a run of lines is a contiguous slice of
// the buffer and a candidate window is hashed in place instead of being rebuilt.
type lineIndex struct {
	data   []byte
	starts []int
}

// indexLines normalizes CRLF to LF and drops one trailing newline so a file's
// final line break does not affect any digest.
func indexLines(data []byte) lineIndex {
	normalized := data
	if bytes.Contains(normalized, []byte("\r\n")) {
		normalized = bytes.ReplaceAll(normalized, []byte("\r\n"), []byte("\n"))
	}
	normalized = bytes.TrimSuffix(normalized, []byte("\n"))
	starts := make([]int, 1, bytes.Count(normalized, []byte("\n"))+1)
	for offset := 0; ; {
		next := bytes.IndexByte(normalized[offset:], '\n')
		if next < 0 {
			break
		}
		offset += next + 1
		starts = append(starts, offset)
	}
	return lineIndex{data: normalized, starts: starts}
}

func (ix lineIndex) count() int { return len(ix.starts) }

// region returns the raw bytes of a 1-based inclusive line range, excluding the
// newline that terminates the last line.
func (ix lineIndex) region(lineStart, lineEnd int) ([]byte, bool) {
	if lineStart < 1 || lineEnd < lineStart || lineEnd > len(ix.starts) {
		return nil, false
	}
	end := len(ix.data)
	if lineEnd < len(ix.starts) {
		end = ix.starts[lineEnd] - 1
	}
	return ix.data[ix.starts[lineStart-1]:end], true
}

// hash digests a 1-based inclusive line range.
func (ix lineIndex) hash(lineStart, lineEnd int) (string, bool) {
	region, ok := ix.region(lineStart, lineEnd)
	if !ok {
		return "", false
	}
	sum := sha256.Sum256(region)
	return "sha256:" + hex.EncodeToString(sum[:]), true
}

// HashRegion returns a SHA-256 hash for a 1-based inclusive line range. CRLF
// and LF are normalized to LF; a final newline does not affect the digest.
func HashRegion(data []byte, lineStart, lineEnd int) (string, error) {
	ix := indexLines(data)
	sum, ok := ix.hash(lineStart, lineEnd)
	if !ok {
		return "", fmt.Errorf("line range %d-%d is outside 1-%d", lineStart, lineEnd, ix.count())
	}
	return sum, nil
}

// maxRelocationBytes bounds the sliding-window relocation search. Relocation
// only runs for an anchor that already failed its hash check, and the search
// walks outward from the anchor's recorded position, so the usual answer costs a
// handful of windows. The cap keeps a pathological case (a huge region in a huge
// file) from turning a lint pass into a stall; hitting it leaves the anchor
// stale, which is the honest answer rather than a slow one.
const maxRelocationBytes = 32 << 20

// relocateRegion looks for span consecutive lines whose digest equals want,
// searching outward from origin so the nearest match to the anchor's recorded
// position wins. That ordering matters: identical regions do repeat in a file
// (boilerplate, table rows), and the closest one is both the likeliest
// relocation and a deterministic choice. At equal distance the later position is
// tried first, because code grows above a region more often than it shrinks. The
// returned range is 1-based and inclusive.
func relocateRegion(ix lineIndex, want string, span, origin int) (int, int, bool) {
	total := ix.count()
	if want == "" || span < 1 || span > total {
		return 0, 0, false
	}
	last := total - span + 1
	if origin < 1 {
		origin = 1
	} else if origin > last {
		origin = last
	}
	budget := maxRelocationBytes
	for offset := 0; ; offset++ {
		below, above := origin-offset, origin+offset
		if below < 1 && above > last {
			return 0, 0, false
		}
		candidates := [2]int{above, below}
		probes := candidates[:]
		if offset == 0 {
			probes = candidates[:1]
		}
		for _, start := range probes {
			if start < 1 || start > last {
				continue
			}
			region, ok := ix.region(start, start+span-1)
			if !ok {
				continue
			}
			if budget -= len(region); budget < 0 {
				return 0, 0, false
			}
			sum := sha256.Sum256(region)
			if "sha256:"+hex.EncodeToString(sum[:]) == want {
				return start, start + span - 1, true
			}
		}
	}
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
		if anchor.Symbol == "" {
			result.Status = AnchorInvalid
			result.Message = "anchor has neither a resolvable symbol nor a line range"
			return result
		}
		// The symbol name no longer resolves, but a renamed or moved symbol whose
		// body is untouched still backs the note. Recover it by content when the
		// anchor also recorded a line range to size the search.
		if anchor.LineStart >= 1 && anchor.LineEnd >= anchor.LineStart {
			ix := indexLines(data)
			span := anchor.LineEnd - anchor.LineStart + 1
			if movedStart, movedEnd, found := relocateRegion(ix, anchor.ContentHash, span, anchor.LineStart); found {
				result.Status = AnchorMoved
				result.MovedStart, result.MovedEnd = movedStart, movedEnd
				result.Actual = anchor.ContentHash
				result.Message = fmt.Sprintf("symbol %s no longer resolves; its anchored lines are intact at %d-%d", anchor.Symbol, movedStart, movedEnd)
				return result
			}
		}
		result.Status = AnchorMissing
		result.Message = "symbol not found: " + anchor.Symbol
		return result
	}
	ix := indexLines(data)
	actual, ok := ix.hash(start, end)
	if !ok {
		result.Status = AnchorInvalid
		result.Message = fmt.Sprintf("line range %d-%d is outside 1-%d", start, end, ix.count())
		return result
	}
	result.Actual = actual
	if actual == anchor.ContentHash {
		result.Status = AnchorCurrent
		return result
	}
	// The region at those coordinates changed, but the anchored lines may simply
	// have shifted. Finding them intact elsewhere keeps the note usable and tells
	// the caller exactly which range to cite and repair.
	if movedStart, movedEnd, found := relocateRegion(ix, anchor.ContentHash, end-start+1, start); found {
		result.Status = AnchorMoved
		result.MovedStart, result.MovedEnd = movedStart, movedEnd
		result.Actual = anchor.ContentHash
		result.Message = fmt.Sprintf("anchored source region moved to lines %d-%d", movedStart, movedEnd)
		return result
	}
	result.Status = AnchorStale
	result.Message = "anchored source region changed"
	return result
}
