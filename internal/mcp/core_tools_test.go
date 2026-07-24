package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/capability"
	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestCoreToolsAreStructuredAndProposalRemainsReviewOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".prowl"), 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(filepath.Join(root, ".prowl", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := knowledge.NewRepository(filepath.Join(root, ".prowl", "knowledge"), okfv01.Codec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	catalog, err := capability.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	contextService := &contextpacket.Service{Store: database, Knowledge: repository, Root: root}
	server := NewServerWithOptions(query.New(database), database, "test", nil, nil, nil, ServerOptions{Surface: SurfaceCore, Context: contextService, Knowledge: repository, Capabilities: catalog, Root: root})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "tool-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "propose_knowledge_change" && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint) {
			t.Fatalf("read-only annotations missing for %s: %+v", tool.Name, tool.Annotations)
		}
	}
	capabilities, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "search_capabilities", Arguments: map[string]any{"query": "context"}})
	if err != nil || capabilities.IsError || capabilities.StructuredContent == nil {
		t.Fatalf("capability result = %+v err=%v", capabilities, err)
	}
	if len(capabilities.Content) == 0 {
		t.Fatal("core capability result omitted MCP resource links")
	}
	if _, ok := capabilities.Content[0].(*sdk.ResourceLink); !ok {
		t.Fatalf("capability content[0] = %T, want *mcp.ResourceLink", capabilities.Content[0])
	}
	candidate := "---\ntype: Decision\ntitle: Keep review human\nprowl:\n  id: review-human\n---\nNever accept from MCP.\n"
	denied, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "propose_knowledge_change", Arguments: map[string]any{"target": "decisions/review.md", "candidate": candidate}})
	if err != nil {
		t.Fatal(err)
	}
	if !denied.IsError {
		t.Fatal("non-elicitation client created a proposal")
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".prowl", "proposals")); err == nil && len(entries) > 0 {
		t.Fatalf("denied proposal wrote files: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(repository.Root, "decisions", "review.md")); !os.IsNotExist(err) {
		t.Fatal("MCP proposal accepted canonical knowledge")
	}
}

func TestContextLensParityMCP(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".prowl"), 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(filepath.Join(root, ".prowl", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := knowledge.NewRepository(filepath.Join(root, ".prowl", "knowledge"), okfv01.Codec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}

	contextService := &contextpacket.Service{Store: database, Knowledge: repository, Root: root}
	input := contextSearchIn{Query: "authentication boundary"}
	raw, err := contextService.Search(contextpacket.Request{Question: input.Query, Mode: contextpacket.ModeCompact, BudgetTokens: 1800})
	if err != nil {
		t.Fatal(err)
	}
	want, err := contextpacket.MarshalCanonicalProjection(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, packet, err := (&handlers{context: contextService}).searchContext(context.Background(), nil, input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	if packet.TraceID != "" || bytes.Contains(encoded, []byte(`"trace_id"`)) || !bytes.Equal(encoded, want) {
		t.Fatalf("MCP canonical packet differs:\n got: %s\nwant: %s", encoded, want)
	}
}
