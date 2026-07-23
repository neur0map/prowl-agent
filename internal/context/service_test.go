package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestServicePrefersHealthyKnowledgeAndFallsBackToChangedSource(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source := []byte("authentication tokens are validated locally\n")
	if err := os.WriteFile(filepath.Join(root, "auth.txt"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	fileID, err := db.UpsertFile(store.File{RelPath: "auth.txt", Lang: "text", Hash: "hash", Size: int64(len(source)), MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceFileGraph(fileID, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 1, Text: string(source)}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := knowledge.HashRegion(source, 1, 1)
	repo := knowledge.NewRepository(filepath.Join(root, "knowledge"), okfv01.Codec{})
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	doc, err := okfv01.Codec{}.Parse("authentication.md", []byte("---\ntype: Concept\ntitle: Authentication tokens\ndescription: Authentication tokens are validated locally.\nprowl:\n  id: authentication\n  confidence: verified\n  anchors:\n    - path: auth.txt\n      line_start: 1\n      line_end: 1\n      content_hash: "+hash+"\n---\nUse the local validator before accepting a token.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Write(doc); err != nil {
		t.Fatal(err)
	}
	service := Service{Store: db, Knowledge: repo, Root: root}
	request := Request{Question: "authentication tokens", Mode: ModeCompact, BudgetTokens: 500}
	healthy, err := service.Search(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthy.Items) < 2 || healthy.Items[0].ID != "concept:authentication" || healthy.Items[0].Freshness != "current" {
		t.Fatalf("healthy ranking = %+v", healthy.Items)
	}
	if err := os.WriteFile(filepath.Join(root, "auth.txt"), []byte("authentication tokens now use a remote validator\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := service.Search(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale.Items) < 2 || !strings.HasPrefix(stale.Items[0].ID, "source:") || stale.Items[1].ID != "concept:authentication" || stale.Items[1].Freshness != "stale" {
		t.Fatalf("stale fallback ranking = %+v", stale.Items)
	}
	get, err := service.Get(Request{IDs: []string{stale.Items[0].ID, "concept:authentication", "missing"}, Mode: ModeStandard, BudgetTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(get.Items) != 2 || get.Omitted["not_found"] != 1 || get.TraceID == "" {
		t.Fatalf("get packet = %+v", get)
	}
}

func TestServiceRequiresQuestionAndIDs(t *testing.T) {
	service := Service{}
	if _, err := service.Search(Request{}); err == nil {
		t.Fatal("empty search accepted")
	}
	if _, err := service.Get(Request{}); err == nil {
		t.Fatal("empty get accepted")
	}
}
