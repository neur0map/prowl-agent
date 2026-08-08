package mcp

import (
	"encoding/json"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	toon "github.com/toon-format/toon-go"
)

// toonFallback renders a tool's structured output as TOON for the model-readable
// content block. The SDK otherwise fills an empty content with the JSON encoding
// of the output; TOON carries the same data for roughly 40% fewer tokens, which
// matters because the model reads this on every tool call. The structured output
// still travels as JSON in StructuredContent for clients that parse it.
//
// It returns nil when rendering fails or is empty, so the caller leaves the
// result untouched and the SDK falls back to its JSON content.
func toonFallback(out any) *sdk.CallToolResult {
	data, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil
	}
	text, err := toon.MarshalString(generic)
	if err != nil || strings.TrimSpace(text) == "" {
		return nil
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}
