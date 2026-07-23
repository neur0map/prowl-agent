package mcp

import (
	"context"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromptsUseProgressiveContextInsteadOfInliningSource(t *testing.T) {
	server := NewServerWithOptions(nil, nil, "test", nil, nil, nil, ServerOptions{Surface: SurfaceCore})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "prompt-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(listed.Prompts))
	for index, prompt := range listed.Prompts {
		names[index] = prompt.Name
	}
	sort.Strings(names)
	want := []string{"prepare-implementation", "research-topic", "review-change", "understand-project", "update-knowledge"}
	if !equalStrings(names, want) {
		t.Fatalf("prompts = %v, want %v", names, want)
	}
	result, err := session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "research-topic", Arguments: map[string]string{"topic": "storage migrations"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %+v", result.Messages)
	}
	text, ok := result.Messages[0].Content.(*sdk.TextContent)
	if !ok {
		t.Fatalf("content type = %T", result.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "storage migrations") || !strings.Contains(text.Text, "search_context") || !strings.Contains(text.Text, "prowl://workspace/current") {
		t.Fatalf("prompt does not use progressive context: %s", text.Text)
	}
	if len(text.Text) > 2000 {
		t.Fatalf("prompt is unbounded: %d bytes", len(text.Text))
	}
}
