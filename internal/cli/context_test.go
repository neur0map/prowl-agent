package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func TestContextCLIEmitsSharedPacket(t *testing.T) {
	root := t.TempDir()
	_, err := workspace.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	text := "package fixture\n\n// bounded context packets include citations\nfunc Context() {}\n"
	if err := os.WriteFile(filepath.Join(root, "context.go"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if len(packet.Items) != 1 || packet.Items[0].Citations[0].Path != filepath.ToSlash("context.go") || packet.TraceID == "" {
		t.Fatalf("packet = %+v", packet)
	}
	updated := "package fixture\n\n// beta newvalue proves the live source was refreshed\nfunc Context() {}\n"
	if err := os.WriteFile(filepath.Join(root, "context.go"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "context.go"), future, future); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	command = newContextSearchCmd()
	command.SetOut(&output)
	command.SetArgs([]string{"newvalue", "--json", "--budget-tokens", "100"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &packet); err != nil || len(packet.Items) == 0 || packet.Items[0].Citations[0].Path != "context.go" || packet.Items[0].Freshness != "current" {
		t.Fatalf("stale packet = %+v err=%v output=%q", packet, err, output.String())
	}

	traceCommand := newContextTracesCmd()
	output.Reset()
	traceCommand.SetOut(&output)
	traceCommand.SetArgs([]string{"--json", "--limit", "5"})
	if err := traceCommand.Execute(); err != nil {
		t.Fatal(err)
	}
	var runs []store.ContextRun
	if err := json.Unmarshal(output.Bytes(), &runs); err != nil || len(runs) != 2 {
		t.Fatalf("trace JSON = %q err=%v", output.String(), err)
	}
	if runs[0].ID != packet.TraceID || runs[0].QueryHash == "" || bytes.Contains(output.Bytes(), []byte("bounded context")) {
		t.Fatalf("unsafe trace output = %q", output.String())
	}

	command = newContextSearchCmd()
	command.SetOut(&output)
	command.SetArgs([]string{"bounded context", "--mode", "full", "--budget-tokens", "0"})
	if err := command.Execute(); err == nil {
		t.Fatal("unbounded full CLI request accepted")
	}
}

func TestOpenContextServiceReturnsMalformedConfigError(t *testing.T) {
	root := t.TempDir()
	state, err := workspace.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.Path, "config.toml"), []byte("languages = [\"go\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	service, closeProject, err := openContextService(context.Background())
	if err == nil {
		if closeProject != nil {
			closeProject()
		}
		t.Fatalf("openContextService = %v, want malformed config error", service)
	}
	if !strings.Contains(err.Error(), "load project config") {
		t.Fatalf("error = %q, want config context", err)
	}
}
