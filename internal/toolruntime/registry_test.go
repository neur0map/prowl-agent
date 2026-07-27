package toolruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/toolruntime"
)

const objectSchema = `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]}`

func okHandler(_ context.Context, _ json.RawMessage) (toolruntime.Result, error) {
	return toolruntime.Result{Content: "ok"}, nil
}

func readOnlyTool(name string) toolruntime.Tool {
	return toolruntime.Tool{
		Name:        name,
		Description: name,
		Schema:      json.RawMessage(objectSchema),
		Permissions: []toolruntime.PermissionClass{toolruntime.PermissionReadOnly},
		Bounds:      toolruntime.Bounds{MaxInputBytes: 4096, MaxOutputBytes: 65536},
		Handler:     okHandler,
	}
}

func mustRegister(t *testing.T, reg *toolruntime.Registry, tool toolruntime.Tool) {
	t.Helper()
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register %s: %v", tool.Name, err)
	}
}

func TestRegistryRegisterFailsClosed(t *testing.T) {
	reg := toolruntime.NewRegistry()
	mustRegister(t, reg, readOnlyTool("a"))

	dup := readOnlyTool("a")
	if err := reg.Register(dup); !errors.Is(err, toolruntime.ErrDuplicateTool) {
		t.Fatalf("duplicate name = %v", err)
	}

	cases := map[string]func(toolruntime.Tool) toolruntime.Tool{
		"invalid json schema": func(tool toolruntime.Tool) toolruntime.Tool { tool.Schema = json.RawMessage(`{`); return tool },
		"non-object schema":   func(tool toolruntime.Tool) toolruntime.Tool { tool.Schema = json.RawMessage(`[]`); return tool },
		"typeless schema": func(tool toolruntime.Tool) toolruntime.Tool {
			tool.Schema = json.RawMessage(`{"properties":{}}`)
			return tool
		},
		"empty schema": func(tool toolruntime.Tool) toolruntime.Tool { tool.Schema = nil; return tool },
	}
	for name, mutate := range cases {
		tool := mutate(readOnlyTool("schema-" + name))
		if err := reg.Register(tool); !errors.Is(err, toolruntime.ErrInvalidSchema) {
			t.Fatalf("%s = %v", name, err)
		}
	}

	toolCases := map[string]func(toolruntime.Tool) toolruntime.Tool{
		"empty permissions": func(tool toolruntime.Tool) toolruntime.Tool { tool.Permissions = nil; return tool },
		"invalid permission": func(tool toolruntime.Tool) toolruntime.Tool {
			tool.Permissions = []toolruntime.PermissionClass{"telepathy"}
			return tool
		},
		"missing handler":    func(tool toolruntime.Tool) toolruntime.Tool { tool.Handler = nil; return tool },
		"nonpositive bounds": func(tool toolruntime.Tool) toolruntime.Tool { tool.Bounds = toolruntime.Bounds{}; return tool },
		"empty name":         func(tool toolruntime.Tool) toolruntime.Tool { tool.Name = ""; return tool },
	}
	for name, mutate := range toolCases {
		tool := mutate(readOnlyTool("tool-" + name))
		if err := reg.Register(tool); !errors.Is(err, toolruntime.ErrInvalidTool) {
			t.Fatalf("%s = %v", name, err)
		}
	}
}

func TestRegistryStableSchemaHash(t *testing.T) {
	reg := toolruntime.NewRegistry()
	// Register with deliberately non-canonical key order; the registry must
	// canonicalize to a stable byte form and reproducible hash.
	tool := readOnlyTool("search")
	tool.Schema = json.RawMessage(`{"properties":{"query":{"type":"string"}},"type":"object"}`)
	mustRegister(t, reg, tool)
	def, ok := reg.Definition("search")
	if !ok {
		t.Fatal("definition missing")
	}
	if def.SchemaHash == "" {
		t.Fatal("empty schema hash")
	}
	// canonical bytes must be valid JSON and stable regardless of input order.
	other := toolruntime.NewRegistry()
	tool2 := readOnlyTool("search")
	tool2.Schema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	mustRegister(t, other, tool2)
	def2, _ := other.Definition("search")
	if def.SchemaHash != def2.SchemaHash {
		t.Fatalf("schema hash unstable across key order: %q vs %q", def.SchemaHash, def2.SchemaHash)
	}
	if !json.Valid(def.Schema) {
		t.Fatalf("canonical schema not valid JSON: %s", def.Schema)
	}
}

func TestPermissionDenied(t *testing.T) {
	reg := toolruntime.NewRegistry()
	mustRegister(t, reg, readOnlyTool("read_source"))

	write := readOnlyTool("apply_patch")
	write.Permissions = []toolruntime.PermissionClass{toolruntime.PermissionWrite}
	mustRegister(t, reg, write)

	network := readOnlyTool("fetch_url")
	network.Permissions = []toolruntime.PermissionClass{toolruntime.PermissionNetwork}
	mustRegister(t, reg, network)

	process := readOnlyTool("run_shell")
	process.Permissions = []toolruntime.PermissionClass{toolruntime.PermissionProcess}
	mustRegister(t, reg, process)

	composite := readOnlyTool("read_then_write")
	composite.Permissions = []toolruntime.PermissionClass{toolruntime.PermissionReadOnly, toolruntime.PermissionWrite}
	mustRegister(t, reg, composite)

	pinned := reg.PinAll()
	grant := toolruntime.ReadOnlyGrant()
	ctx := context.Background()
	input := json.RawMessage(`{"query":"x"}`)

	if res, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: "read_source", Input: input}); err != nil || res.Content != "ok" {
		t.Fatalf("read-only execute = %+v %v", res, err)
	}

	if _, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: "no_such_tool", Input: input}); !errors.Is(err, toolruntime.ErrToolNotFound) {
		t.Fatalf("unknown tool denial = %v", err)
	}

	for _, name := range []string{"apply_patch", "fetch_url", "run_shell", "read_then_write"} {
		if _, err := reg.Execute(ctx, pinned, grant, toolruntime.Call{Name: name, Input: input}); !errors.Is(err, toolruntime.ErrPermissionDenied) {
			t.Fatalf("%s not denied: %v", name, err)
		}
	}

	// A widened composite request over an otherwise read-only tool is denied.
	widened := toolruntime.Call{Name: "read_source", Input: input, Requested: []toolruntime.PermissionClass{toolruntime.PermissionWrite}}
	if _, err := reg.Execute(ctx, pinned, grant, widened); !errors.Is(err, toolruntime.ErrPermissionDenied) {
		t.Fatalf("widened request not denied: %v", err)
	}
}

func TestPermissionDeniedBeforeHandlerSideEffect(t *testing.T) {
	reg := toolruntime.NewRegistry()
	ran := false
	write := readOnlyTool("apply_patch")
	write.Permissions = []toolruntime.PermissionClass{toolruntime.PermissionWrite}
	write.Handler = func(_ context.Context, _ json.RawMessage) (toolruntime.Result, error) {
		ran = true
		return toolruntime.Result{Content: "mutated"}, nil
	}
	mustRegister(t, reg, write)

	_, err := reg.Execute(context.Background(), reg.PinAll(), toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: "apply_patch", Input: json.RawMessage(`{"query":"x"}`)})
	if !errors.Is(err, toolruntime.ErrPermissionDenied) {
		t.Fatalf("expected denied, got %v", err)
	}
	if ran {
		t.Fatal("handler executed despite permission denial (side effect before deny)")
	}
}

func TestRegistryPinnedToolsetIsolation(t *testing.T) {
	reg := toolruntime.NewRegistry()
	mustRegister(t, reg, readOnlyTool("search_context"))
	pinned := reg.PinAll() // pins {search_context}

	// A tool added after the session pinned its toolset must not be resolvable.
	mustRegister(t, reg, readOnlyTool("late_tool"))
	ctx := context.Background()
	input := json.RawMessage(`{"query":"x"}`)
	if _, err := reg.Execute(ctx, pinned, toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: "late_tool", Input: input}); !errors.Is(err, toolruntime.ErrToolNotFound) {
		t.Fatalf("stale pin resolved a newly added tool: %v", err)
	}
	// A fresh pin does see the new tool.
	if _, err := reg.Execute(ctx, reg.PinAll(), toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: "late_tool", Input: input}); err != nil {
		t.Fatalf("fresh pin cannot resolve late tool: %v", err)
	}
	// Pinning an unknown name fails closed.
	if _, err := reg.Pin("does_not_exist"); err == nil {
		t.Fatal("Pin of unknown tool did not fail")
	}
}

func TestRegistryExecuteInputBound(t *testing.T) {
	reg := toolruntime.NewRegistry()
	tool := readOnlyTool("read_source")
	tool.Bounds = toolruntime.Bounds{MaxInputBytes: 8, MaxOutputBytes: 32}
	mustRegister(t, reg, tool)
	if _, err := reg.Execute(context.Background(), reg.PinAll(), toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: "read_source", Input: json.RawMessage(`{"query":"way too long"}`)}); !errors.Is(err, toolruntime.ErrInputTooLarge) {
		t.Fatalf("oversize input = %v", err)
	}
}

func TestRegistryExecuteOutputBound(t *testing.T) {
	reg := toolruntime.NewRegistry()
	tool := readOnlyTool("read_source")
	tool.Bounds = toolruntime.Bounds{MaxInputBytes: 4096, MaxOutputBytes: 16}
	tool.Handler = func(_ context.Context, _ json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Content: strings.Repeat("A", 1024)}, nil
	}
	mustRegister(t, reg, tool)
	res, err := reg.Execute(context.Background(), reg.PinAll(), toolruntime.ReadOnlyGrant(), toolruntime.Call{Name: "read_source", Input: json.RawMessage(`{"query":"x"}`)})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Content) > 16 {
		t.Fatalf("output not bounded: len=%d", len(res.Content))
	}
}

func TestRegistryDefinitionsSorted(t *testing.T) {
	reg := toolruntime.NewRegistry()
	mustRegister(t, reg, readOnlyTool("gamma"))
	mustRegister(t, reg, readOnlyTool("alpha"))
	mustRegister(t, reg, readOnlyTool("beta"))
	got := reg.Names()
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v want %v", got, want)
	}
}
