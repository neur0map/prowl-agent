package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestMCPIntegration(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := index.Index(s, filepath.Join("..", "..", "testdata", "rice-hypr"), nil); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(query.New(s), "test",
		func(context.Context) (string, error) { return "reindexed", nil },
		func(context.Context) (doctor.Report, error) { return doctor.Run(s, config.Rules{}, doctor.Options{}) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverT, clientT := sdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverT) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("call %s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("tool %s returned error: %+v", name, res.Content)
		}
		if len(res.Content) == 0 {
			t.Fatalf("tool %s returned no content", name)
		}
		tc, ok := res.Content[0].(*sdk.TextContent)
		if !ok {
			t.Fatalf("tool %s: content not text: %T", name, res.Content[0])
		}
		return tc.Text
	}

	if out := call("find_symbol", map[string]any{"name": "M.apply"}); !strings.Contains(out, "M.apply") || !strings.Contains(out, "nvim/lua/opts.lua") {
		t.Fatalf("find_symbol: %s", out)
	}
	if out := call("blast_radius", map[string]any{"path": "hypr/colors.conf"}); !strings.Contains(out, "hypr/hyprland.conf") {
		t.Fatalf("blast_radius: %s", out)
	}
	if out := call("find_callers", map[string]any{"path": "hypr/colors.conf"}); !strings.Contains(out, "hypr/hyprland.conf") {
		t.Fatalf("find_callers: %s", out)
	}
	if out := call("status", nil); !strings.Contains(out, "\"files\":11") {
		t.Fatalf("status: %s", out)
	}
	if out := call("reindex", nil); !strings.Contains(out, "reindexed") {
		t.Fatalf("reindex: %s", out)
	}
	if out := call("doctor", nil); !strings.Contains(out, "\"score\"") {
		t.Fatalf("doctor: %s", out)
	}
	if out := call("overview", nil); !strings.Contains(out, "\"roles\"") {
		t.Fatalf("overview: %s", out)
	}
	if out := call("clusters", nil); !strings.Contains(out, "\"clusters\"") {
		t.Fatalf("clusters: %s", out)
	}

	lt, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(lt.Tools) != 17 {
		t.Fatalf("tool count = %d, want 17", len(lt.Tools))
	}
}
