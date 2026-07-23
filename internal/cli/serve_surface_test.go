package cli

import (
	"strings"
	"testing"
)

func TestServeMCPSurfaceDefaultsToLegacyAndRejectsUnknown(t *testing.T) {
	command := newServeCmd("test")
	flag := command.Flags().Lookup("mcp-surface")
	if flag == nil || flag.DefValue != "legacy" {
		t.Fatalf("mcp-surface flag = %+v", flag)
	}
	command.SetArgs([]string{"--mcp-surface", "everything"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown MCP surface") {
		t.Fatalf("invalid surface error = %v", err)
	}
}
