package knowledge_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
)

func TestRepositoryInitWriteListIndexLogAndExport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "knowledge")
	repo := knowledge.NewRepository(root, okfv01.Codec{})
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "index.md")
	if err := os.WriteFile(indexPath, []byte("# My curated index\n\nHuman notes stay.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := okfv01.Codec{}.Parse("architecture/storage.md", []byte("---\ntype: Decision\ntitle: Durable storage\nx-future: 7\nprowl:\n  id: storage-1\n---\nSQLite is derived.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Write(doc); err != nil {
		t.Fatal(err)
	}
	if err := repo.GenerateIndex(); err != nil {
		t.Fatal(err)
	}
	if err := repo.GenerateIndex(); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(indexPath)
	if !bytes.Contains(index, []byte("Human notes stay.")) || strings.Count(string(index), "Durable storage") != 1 {
		t.Fatalf("index did not preserve human content or duplicated generated content:\n%s", index)
	}
	docs, err := repo.List()
	if err != nil || len(docs) != 1 || docs[0].Prowl.ID != "storage-1" {
		t.Fatalf("List = %+v, %v", docs, err)
	}
	at := time.Date(2026, 7, 23, 15, 0, 0, 0, time.FixedZone("local", 3600))
	if err := repo.AppendLog("accepted", doc.Path, at); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(root, "log.md"))
	if !bytes.Contains(log, []byte("2026-07-23T14:00:00Z — accepted `architecture/storage.md`")) {
		t.Fatalf("UTC log entry missing: %s", log)
	}
	export := filepath.Join(t.TempDir(), "export")
	if err := repo.Export(export); err != nil {
		t.Fatal(err)
	}
	exported := knowledge.NewRepository(export, okfv01.Codec{})
	roundTrip, err := exported.Read(doc.Path)
	if err != nil || roundTrip.Prowl.ID != doc.Prowl.ID {
		t.Fatalf("export round trip = %+v, %v", roundTrip, err)
	}
	mapping := roundTrip.Frontmatter.Content[0]
	foundUnknown := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "x-future" && mapping.Content[i+1].Value == "7" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatal("unknown field lost during repository export")
	}
}

func TestRepositoryImportPreservesSourceAndRejectsCollision(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "candidate.md")
	original := []byte("---\ntype: Note\ntitle: Candidate\n---\nBody.\n")
	if err := os.WriteFile(source, original, 0o640); err != nil {
		t.Fatal(err)
	}
	repo := knowledge.NewRepository(filepath.Join(tmp, "bundle"), okfv01.Codec{})
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Import(source, "notes/candidate.md"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(after, original) {
		t.Fatal("import modified source")
	}
	if _, err := repo.Import(source, "notes/candidate.md"); err == nil {
		t.Fatal("import should reject destination collision")
	}
	if _, err := repo.Import(source, "../outside.md"); err == nil {
		t.Fatal("import should reject path traversal")
	}
}
