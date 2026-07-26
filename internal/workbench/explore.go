package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const MaxExploreResponseBytes = 64 << 10

var ErrTourNotFound = errors.New("guided tour is unavailable")

// Explore is the deterministic, progressive project map shown before detailed source reads.
type Explore struct {
	Workspace       WorkspaceIdentity `json:"workspace"`
	Sections        []ExploreSection  `json:"sections"`
	Tours           []TourSummary     `json:"tours"`
	resourceVersion string
	tourFacts       []ExploreFact
}

// ExploreSection is one stable layer of the project map.
type ExploreSection struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Facts       []ExploreFact `json:"facts"`
}

// ExploreFact is a derived fact with an optional exact project-relative source anchor.
type ExploreFact struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Detail string        `json:"detail"`
	Anchor *SourceAnchor `json:"anchor,omitempty"`
}

// SourceAnchor identifies an exact project-relative line range.
type SourceAnchor struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

// TourSummary permits discovery without duplicating a full guided tour in every explore response.
type TourSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Steps int    `json:"steps"`
}

// GuidedTour is a fixed-order, source-backed path through five to twelve indexed source anchors.
type GuidedTour struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Description     string           `json:"description"`
	Steps           []GuidedTourStep `json:"steps"`
	resourceVersion string
}

// GuidedTourStep points the reader at a specific layer and its facts.
type GuidedTourStep struct {
	Number      int           `json:"number"`
	SectionID   string        `json:"section_id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Facts       []ExploreFact `json:"facts"`
}

// Explore returns the stable project hierarchy from the same bounded Brief projection as the home view.
func (service *Service) Explore(ctx context.Context) (Explore, error) {
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return Explore{}, err
	}
	defer release()
	brief, err := service.Brief(ctx)
	if err != nil {
		return Explore{}, err
	}
	sections := exploreSections(brief)
	tourFacts, err := service.guidedTourFacts(ctx)
	if err != nil {
		return Explore{}, err
	}
	tours := guidedTours(tourFacts)
	summaries := make([]TourSummary, len(tours))
	for index, tour := range tours {
		summaries[index] = TourSummary{ID: tour.ID, Title: tour.Title, Steps: len(tour.Steps)}
	}
	explore := Explore{
		Workspace: brief.Workspace, Sections: sections, Tours: summaries,
		resourceVersion: brief.resourceVersion, tourFacts: tourFacts,
	}
	encoded, err := json.Marshal(explore)
	if err != nil || len(encoded) > MaxExploreResponseBytes {
		return Explore{}, errors.New("project exploration exceeds response bound")
	}
	return explore, nil
}

// GuidedTour returns one deterministic tour selected by its stable identifier.
func (service *Service) GuidedTour(ctx context.Context, id string) (GuidedTour, error) {
	if validateIdentifier(id) != nil {
		return GuidedTour{}, ErrTourNotFound
	}
	explore, err := service.Explore(ctx)
	if err != nil {
		return GuidedTour{}, err
	}
	for _, tour := range guidedTours(explore.tourFacts) {
		if tour.ID == id {
			tour.resourceVersion = explore.resourceVersion
			return tour, nil
		}
	}
	return GuidedTour{}, ErrTourNotFound
}

func exploreSections(brief Brief) []ExploreSection {
	overview := brief.Overview
	guides := make([]ExploreFact, 0, len(overview.Docs))
	for _, item := range overview.Docs {
		guides = append(guides, sourceFact("guide", item, "Guide document"))
	}
	entrypoints := make([]ExploreFact, 0, len(overview.Entrypoints))
	for _, item := range overview.Entrypoints {
		entrypoints = append(entrypoints, sourceFact("entrypoint", item, "Entrypoint"))
	}
	subsystems := make([]ExploreFact, 0, len(overview.Clusters))
	for _, cluster := range overview.Clusters {
		subsystems = append(subsystems, ExploreFact{ID: "subsystem:" + cluster.Label, Label: cluster.Label, Detail: fmt.Sprintf("%s · %d files", cluster.Lang, cluster.Files)})
	}
	hotspots := make([]ExploreFact, 0, len(overview.Hotspots))
	for _, hotspot := range overview.Hotspots {
		hotspots = append(hotspots, sourceFact("hotspot", hotspot.File, fmt.Sprintf("%d incoming links", hotspot.In)))
	}
	capabilities := make([]ExploreFact, 0, len(brief.Capabilities))
	for _, capability := range brief.Capabilities {
		capabilities = append(capabilities, ExploreFact{ID: "capability:" + capability.Name, Label: capability.Title, Detail: capability.Description})
	}
	return []ExploreSection{
		{ID: "guides", Title: "Guides", Description: fmt.Sprintf("%d guide documents", len(guides)), Facts: guides},
		{ID: "entrypoints", Title: "Entrypoints", Description: fmt.Sprintf("%d entrypoints", len(entrypoints)), Facts: entrypoints},
		{ID: "subsystems", Title: "Subsystems", Description: fmt.Sprintf("%d connected subsystems", len(subsystems)), Facts: subsystems},
		{ID: "hotspots", Title: "Review hotspots", Description: fmt.Sprintf("%d dependency hotspots", len(hotspots)), Facts: hotspots},
		{ID: "capabilities", Title: "Capabilities", Description: fmt.Sprintf("%d local workflows", len(capabilities)), Facts: capabilities},
	}
}

func sourceFact(kind, path, detail string) ExploreFact {
	return ExploreFact{
		ID:     kind + ":" + path,
		Label:  path,
		Detail: detail,
		Anchor: &SourceAnchor{Path: path, LineStart: 1, LineEnd: 1},
	}
}

func (service *Service) guidedTourFacts(ctx context.Context) ([]ExploreFact, error) {
	const (
		minSteps = 5
		maxSteps = 12
	)
	files, err := service.project.Store.AllFilesContext(ctx, maxSteps)
	if err != nil {
		return nil, err
	}
	facts := make([]ExploreFact, 0, maxSteps)
	extras := make([]ExploreFact, 0, minSteps)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if validateRelativePath(file.RelPath, service.workspaceRoots()...) != nil {
			return nil, ErrInvalidDerivedData
		}
		chunk, found, err := service.project.Store.FirstChunk(file.RelPath)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		lines := strings.Split(strings.TrimSuffix(chunk.Text, "\n"), "\n")
		for index := range lines {
			line := chunk.StartLine + index
			fact := ExploreFact{
				ID:     fmt.Sprintf("source:%s:%d", file.RelPath, line),
				Label:  file.RelPath,
				Detail: fmt.Sprintf("%s source evidence at line %d", file.Lang, line),
				Anchor: &SourceAnchor{Path: file.RelPath, LineStart: line, LineEnd: line},
			}
			if index == 0 {
				facts = append(facts, fact)
			} else {
				extras = append(extras, fact)
			}
		}
	}
	for len(facts) < minSteps && len(extras) > 0 {
		facts = append(facts, extras[0])
		extras = extras[1:]
	}
	seedCount := len(facts)
	for len(facts) < minSteps && seedCount > 0 {
		duplicate := facts[len(facts)%seedCount]
		duplicate.ID = fmt.Sprintf("%s:step-%d", duplicate.ID, len(facts)+1)
		facts = append(facts, duplicate)
	}
	if len(facts) < minSteps {
		return nil, errors.New("guided tour requires indexed source evidence")
	}
	if len(facts) > maxSteps {
		facts = facts[:maxSteps]
	}
	return facts, nil
}

func guidedTours(facts []ExploreFact) []GuidedTour {
	reversed := append([]ExploreFact(nil), facts...)
	slices.Reverse(reversed)
	rotated := append([]ExploreFact(nil), facts[1:]...)
	rotated = append(rotated, facts[0])
	return []GuidedTour{
		newGuidedTour("onboarding", "Start with the project", "Follow indexed project evidence in deterministic source order.", facts),
		newGuidedTour("architecture", "Trace the architecture", "Trace indexed source evidence from a second deterministic starting point.", rotated),
		newGuidedTour("review", "Prepare a review", "Review indexed source evidence in deterministic reverse order.", reversed),
	}
}

func newGuidedTour(id, title, description string, facts []ExploreFact) GuidedTour {
	steps := make([]GuidedTourStep, len(facts))
	for index, fact := range facts {
		steps[index] = GuidedTourStep{
			Number: index + 1, SectionID: "source", Title: fact.Label,
			Description: fact.Detail, Facts: []ExploreFact{fact},
		}
	}
	return GuidedTour{ID: id, Title: title, Description: description, Steps: steps}
}
