package toolruntime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	contextpkg "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/toolruntime"
)

func lineCount(body string) int {
	n := strings.Count(body, "\n")
	if len(body) > 0 && !strings.HasSuffix(body, "\n") {
		n++
	}
	if n == 0 {
		n = 1
	}
	return n
}

// newReadOnlyRegistry wires the three read-only tools over the real canonical
// context service (backed by an indexed store) and a rooted source reader.
func newReadOnlyRegistry(t *testing.T, files map[string]string) (*toolruntime.Registry, string) {
	t.Helper()
	srcRoot := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(srcRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for rel, body := range files {
		fileID, err := db.UpsertFile(store.File{RelPath: rel, Lang: "text", Hash: rel, Size: int64(len(body)), MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ReplaceFileGraph(fileID, nil, nil, nil, []store.Chunk{{StartLine: 1, EndLine: lineCount(body), Text: body}}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &contextpkg.Service{Store: db, Root: srcRoot}
	osRoot, err := os.OpenRoot(srcRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = osRoot.Close() })
	reg := toolruntime.NewRegistry()
	cfg := toolruntime.ReadOnlyConfig{
		Context:             svc,
		SourceRoot:          osRoot,
		MaxSourceBytes:      65536,
		MaxSourceLines:      200,
		DefaultBudgetTokens: 500,
		ToolBounds:          toolruntime.Bounds{MaxInputBytes: 8192, MaxOutputBytes: 262144},
	}
	if err := toolruntime.RegisterReadOnlyContext(reg, cfg); err != nil {
		t.Fatal(err)
	}
	return reg, srcRoot
}

func TestReadOnlyContextToolset(t *testing.T) {
	reg, _ := newReadOnlyRegistry(t, map[string]string{"a.txt": "hello world\n"})
	got := reg.Names()
	want := []string{"get_context", "read_source", "search_context"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered tools = %v want %v", got, want)
	}
	for _, def := range reg.Definitions() {
		if len(def.Permissions) != 1 || def.Permissions[0] != toolruntime.PermissionReadOnly {
			t.Fatalf("%s permissions = %v (want read-only only)", def.Name, def.Permissions)
		}
		if def.SchemaHash == "" || !json.Valid(def.Schema) {
			t.Fatalf("%s schema unstable/invalid", def.Name)
		}
	}
}

func TestReadOnlyContextSearchAndGet(t *testing.T) {
	reg, _ := newReadOnlyRegistry(t, map[string]string{
		"auth.txt": "authentication tokens are validated locally\n",
	})
	pinned := reg.PinAll()
	grant := toolruntime.ReadOnlyGrant()
	ctx := context.Background()

	res, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolSearchContext, Input: json.RawMessage(`{"question":"authentication tokens"}`)})
	if err != nil {
		t.Fatalf("search_context: %v", err)
	}
	var packet contextpkg.Packet
	if err := json.Unmarshal([]byte(res.Content), &packet); err != nil {
		t.Fatalf("decode packet: %v (%s)", err, res.Content)
	}
	if len(packet.Items) == 0 {
		t.Fatalf("no items in packet: %s", res.Content)
	}
	item := packet.Items[0]
	if len(item.Citations) == 0 || item.Citations[0].Path != "auth.txt" {
		t.Fatalf("citations not retained: %+v", item.Citations)
	}
	if item.Freshness == "" {
		t.Fatal("freshness not retained")
	}
	if packet.Budget.EstimatedTokens == 0 && packet.Budget.EstimatedBytes == 0 {
		t.Fatal("budget/bounds not retained")
	}

	res2, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolGetContext, Input: json.RawMessage(`{"ids":["` + item.ID + `"]}`)})
	if err != nil {
		t.Fatalf("get_context: %v", err)
	}
	var packet2 contextpkg.Packet
	if err := json.Unmarshal([]byte(res2.Content), &packet2); err != nil {
		t.Fatalf("decode get packet: %v", err)
	}
	if len(packet2.Items) != 1 || packet2.Items[0].ID != item.ID {
		t.Fatalf("get returned = %+v", packet2.Items)
	}

	res3, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolGetContext, Input: json.RawMessage(`{"ids":["source:unknownid"]}`)})
	if err != nil {
		t.Fatalf("get_context missing id: %v", err)
	}
	var packet3 contextpkg.Packet
	if err := json.Unmarshal([]byte(res3.Content), &packet3); err != nil {
		t.Fatalf("decode omission packet: %v", err)
	}
	if packet3.Omitted["not_found"] == 0 {
		t.Fatalf("omissions not retained: %+v", packet3.Omitted)
	}
}

func TestReadOnlyContextSearchRejectsBlankQuestion(t *testing.T) {
	reg, _ := newReadOnlyRegistry(t, map[string]string{"a.txt": "hello\n"})
	res, err := reg.Execute(context.Background(), reg.PinAll(), toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: toolruntime.ToolSearchContext, Input: json.RawMessage(`{"question":"   "}`)})
	if err == nil {
		t.Fatalf("blank question accepted: %+v", res)
	}
}

func TestReadOnlyContextReadSource(t *testing.T) {
	reg, _ := newReadOnlyRegistry(t, map[string]string{
		"src/lib.txt": "line1\nline2\nline3\nline4\n",
	})
	pinned := reg.PinAll()
	grant := toolruntime.ReadOnlyGrant()
	ctx := context.Background()

	res, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolReadSource, Input: json.RawMessage(`{"path":"src/lib.txt"}`)})
	if err != nil {
		t.Fatalf("read whole: %v", err)
	}
	if !strings.Contains(res.Content, "line1") || !strings.Contains(res.Content, "line4") {
		t.Fatalf("whole file content = %q", res.Content)
	}

	res2, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolReadSource, Input: json.RawMessage(`{"path":"src/lib.txt","line_start":2,"line_end":3}`)})
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if strings.Contains(res2.Content, "line1") || strings.Contains(res2.Content, "line4") {
		t.Fatalf("range leaked out-of-range lines: %q", res2.Content)
	}
	if !strings.Contains(res2.Content, "line2") || !strings.Contains(res2.Content, "line3") {
		t.Fatalf("range missing requested lines: %q", res2.Content)
	}
}

func TestReadOnlyContextReadSourceDenied(t *testing.T) {
	reg, srcRoot := newReadOnlyRegistry(t, map[string]string{"ok.txt": "hello\n"})
	if err := os.Mkdir(filepath.Join(srcRoot, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("SECRETVALUE"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(outside, filepath.Join(srcRoot, "escape"))
	if err := os.WriteFile(filepath.Join(srcRoot, "big.txt"), bytes.Repeat([]byte("A"), 200000), 0o644); err != nil {
		t.Fatal(err)
	}

	pinned := reg.PinAll()
	grant := toolruntime.ReadOnlyGrant()
	ctx := context.Background()
	cases := map[string]string{
		"absolute":  `{"path":"/etc/passwd"}`,
		"traversal": `{"path":"../secret.txt"}`,
		"symlink":   `{"path":"escape"}`,
		"directory": `{"path":"adir"}`,
		"too-large": `{"path":"big.txt"}`,
		"empty":     `{"path":""}`,
	}
	for name, input := range cases {
		res, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: toolruntime.ToolReadSource, Input: json.RawMessage(input)})
		if err == nil {
			t.Fatalf("%s: expected denial, got content %q", name, res.Content)
		}
		if strings.Contains(res.Content, "SECRETVALUE") {
			t.Fatalf("%s: leaked out-of-root secret", name)
		}
	}
}
