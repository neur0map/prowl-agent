package knowledge

import "fmt"

// AnchorRepair records one anchor whose recorded line range was corrected after
// its content was found intact elsewhere in the same file.
type AnchorRepair struct {
	// Document is the bundle-relative knowledge document that owns the anchor.
	Document string `json:"document"`
	// Source is the anchored source file, repository-relative.
	Source string `json:"source"`
	// Symbol is the anchor's symbol name when it pins one, else empty.
	Symbol    string `json:"symbol,omitempty"`
	FromStart int    `json:"from_start"`
	FromEnd   int    `json:"from_end"`
	ToStart   int    `json:"to_start"`
	ToEnd     int    `json:"to_end"`
}

// String renders one repair as a single reviewable line.
func (r AnchorRepair) String() string {
	return fmt.Sprintf("%s: %s:%d-%d -> %d-%d", r.Document, r.Source, r.FromStart, r.FromEnd, r.ToStart, r.ToEnd)
}

// RepairAnchors rewrites the line range of every anchor whose content moved,
// leaving genuinely changed, missing, and invalid anchors untouched for review.
// Only the coordinates are rewritten: the content hash still describes the same
// lines, so a repaired anchor is verifiable rather than merely silenced. Each
// affected document is rewritten once, atomically, preserving its unknown
// frontmatter. It returns the applied repairs in document order.
func (r *Repository) RepairAnchors(sourceRoot string, resolve SymbolResolver) ([]AnchorRepair, error) {
	docs, err := r.List()
	if err != nil {
		return nil, err
	}
	var repairs []AnchorRepair
	for _, doc := range docs {
		changed := false
		for index := range doc.Prowl.Anchors {
			anchor := &doc.Prowl.Anchors[index]
			check := CheckAnchorResolved(sourceRoot, *anchor, resolve)
			if check.Status != AnchorMoved || check.MovedStart < 1 {
				continue
			}
			repairs = append(repairs, AnchorRepair{
				Document: doc.Path, Source: anchor.Path, Symbol: anchor.Symbol,
				FromStart: anchor.LineStart, FromEnd: anchor.LineEnd,
				ToStart: check.MovedStart, ToEnd: check.MovedEnd,
			})
			anchor.LineStart, anchor.LineEnd = check.MovedStart, check.MovedEnd
			changed = true
		}
		if !changed {
			continue
		}
		if err := r.Write(doc); err != nil {
			return repairs, fmt.Errorf("repair %s: %w", doc.Path, err)
		}
	}
	return repairs, nil
}
