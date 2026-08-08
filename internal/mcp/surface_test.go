package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/capability"
	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestParseSurfaceRejectsUnknownValues(t *testing.T) {
	for _, value := range []string{"legacy", "core", "all"} {
		surface, err := ParseSurface(value)
		if err != nil || string(surface) != value {
			t.Fatalf("ParseSurface(%q) = %q, %v", value, surface, err)
		}
	}
	if _, err := ParseSurface("everything"); err == nil {
		t.Fatal("unknown MCP surface accepted")
	}
}

func TestCoreSurfaceIsSmallerAndLegacyStaysEighteen(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	catalog, err := capability.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	service := &contextpacket.Service{Store: database, Root: t.TempDir()}
	legacy := NewServer(query.New(database), database, "test", nil, nil, nil)
	core := NewServerWithOptions(query.New(database), database, "test", nil, nil, nil, ServerOptions{Surface: SurfaceCore, Context: service, Capabilities: catalog})

	legacyTools := listToolDescriptors(t, legacy)
	coreTools := listToolDescriptors(t, core)
	if len(legacyTools) != 18 {
		t.Fatalf("legacy tool count = %d, want 18", len(legacyTools))
	}
	want := []string{"analyze_change", "get_context", "outline", "propose_knowledge_change", "read_symbol", "search_capabilities", "search_context", "validate_knowledge"}
	got := make([]string, len(coreTools))
	for index, tool := range coreTools {
		got[index] = tool.Name
	}
	sort.Strings(got)
	if !equalStrings(got, want) {
		t.Fatalf("core tools = %v, want %v", got, want)
	}
	legacyJSON, _ := json.Marshal(legacyTools)
	coreJSON, _ := json.Marshal(coreTools)
	legacyDigest := fmt.Sprintf("%x", sha256.Sum256(legacyJSON))
	if legacyDigest != "0f576b9618f499e971d5ca00343cd9cee8e37ae3b335994ced0644fc03c388f3" {
		t.Fatalf("legacy descriptor digest = %s", legacyDigest)
	}
	if len(coreJSON) >= len(legacyJSON) {
		t.Fatalf("core schema bytes = %d, legacy = %d", len(coreJSON), len(legacyJSON))
	}
}

func listToolDescriptors(t *testing.T, server *sdk.Server) []*sdk.Tool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "v2-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result.Tools
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
