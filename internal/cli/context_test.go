package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func TestContextCLIEmitsSharedPacket(t *testing.T) {
	root := t.TempDir()
	state, err := workspace.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(state.DB)
	if err != nil {
		t.Fatal(err)
	}
	text := "bounded context packets include citations"
	fileID, err := database.UpsertFile(store.File{RelPath: "context.txt", Lang: "text", Hash: "hash", Size: int64(len(text)), MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceFileGraph(fileID, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: 1, Text: text}}); err != nil {
		t.Fatal(err)
	}
	database.Close()
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	command := newContextSearchCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"bounded context", "--json", "--budget-tokens", "100"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var packet contextpacket.Packet
	if err := json.Unmarshal(output.Bytes(), &packet); err != nil {
		t.Fatalf("invalid packet JSON %q: %v", output.String(), err)
	}
	if len(packet.Items) != 1 || packet.Items[0].Citations[0].Path != filepath.ToSlash("context.txt") || packet.TraceID == "" {
		t.Fatalf("packet = %+v", packet)
	}

	command = newContextSearchCmd()
	command.SetOut(&output)
	command.SetArgs([]string{"bounded context", "--mode", "full", "--budget-tokens", "0"})
	if err := command.Execute(); err == nil {
		t.Fatal("unbounded full CLI request accepted")
	}
}
