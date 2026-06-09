package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/alexaandru/go-sitter-forest/lua"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	_ "github.com/mattn/go-sqlite3"
)

// TestCgoStack proves the riskiest dependencies link and run together:
// mattn/go-sqlite3 with FTS5 (-tags sqlite_fts5) + tree-sitter bindings + a grammar.
func TestCgoStack(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE f USING fts5(x)`); err != nil {
		t.Fatalf("fts5 unavailable (build with -tags sqlite_fts5): %v", err)
	}

	p := sitter.NewParser()
	if ok := p.SetLanguage(sitter.NewLanguage(lua.GetLanguage())); !ok {
		t.Fatal("set lua language failed")
	}
	tree, err := p.ParseString(context.Background(), nil, []byte("local x = 1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if got := tree.RootNode().Type(); got != "chunk" {
		t.Fatalf("root type = %q, want chunk", got)
	}
}
