package entity_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/entity"
	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestRepositoryProjectsLegacyAndKnowledgeWithProvenance(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fileID, err := db.UpsertFile(store.File{RelPath: "main.go", Lang: "go", Hash: "file-hash", Size: 20, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceFileGraph(fileID,
		[]store.Symbol{{Name: "main", Kind: "function", Signature: "func main()", StartLine: 3, EndLine: 5}},
		nil, []store.RawEdge{{SrcName: "main", Kind: "calls", Raw: "helper", Line: 4}}, nil); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "knowledge")
	repo := knowledge.NewRepository(bundle, okfv01.Codec{})
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	doc, _ := okfv01.Codec{}.Parse("architecture.md", []byte("---\ntype: Concept\ntitle: Architecture\nprowl:\n  id: architecture\n---\n"))
	if err := repo.Write(doc); err != nil {
		t.Fatal(err)
	}
	if err := repo.SyncStore(db, root, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	generic := entity.Repository{Store: db}
	artifacts, err := generic.Artifacts()
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := generic.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	relations, err := generic.Relations()
	if err != nil {
		t.Fatal(err)
	}
	var legacyArtifact, knowledgeArtifact, legacyNode, knowledgeNode bool
	for _, artifact := range artifacts {
		if artifact.CanonicalPath == "main.go" && artifact.Provenance.System == "legacy" {
			legacyArtifact = true
		}
		if artifact.CanonicalPath == "architecture.md" && artifact.Provenance.System == "okf" {
			knowledgeArtifact = true
		}
	}
	for _, node := range nodes {
		if node.Name == "main" && node.AnchorStart == 3 && node.Provenance.Table == "symbols" {
			legacyNode = true
		}
		if node.Name == "Architecture" && node.StableKey == "concept:architecture" && node.Provenance.System == "okf" {
			knowledgeNode = true
		}
	}
	if !legacyArtifact || !knowledgeArtifact || !legacyNode || !knowledgeNode {
		t.Fatalf("missing projections: artifacts=%+v nodes=%+v", artifacts, nodes)
	}
	if len(relations) != 1 || relations[0].Provenance.Table != "edges" || relations[0].Evidence == "" {
		t.Fatalf("legacy relation provenance lost: %+v", relations)
	}
	if _, err := os.Stat(filepath.Join(root, "main.go")); err == nil {
		t.Fatal("fixture unexpectedly depends on source file bytes")
	}
}
