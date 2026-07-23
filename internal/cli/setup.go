package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	IntegrationAgents   = "agents"
	IntegrationGeneric  = "generic-mcp"
	IntegrationCursor   = "cursor"
	IntegrationVSCode   = "vscode"
	IntegrationOMP      = "omp"
	IntegrationFactory  = "factory"
	IntegrationOpenCode = "opencode"
	IntegrationNeovim   = "neovim"
	IntegrationHelix    = "helix"
)

var allIntegrations = []string{
	IntegrationAgents,
	IntegrationCursor,
	IntegrationFactory,
	IntegrationGeneric,
	IntegrationHelix,
	IntegrationNeovim,
	IntegrationOMP,
	IntegrationOpenCode,
	IntegrationVSCode,
}

type SetupAction struct {
	Integration string `json:"integration"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type SetupPlan struct {
	Root         string        `json:"root"`
	Integrations []string      `json:"integrations"`
	Actions      []SetupAction `json:"actions"`
}

func DetectIntegrations(root string) []string {
	checks := []struct {
		name string
		path string
	}{
		{IntegrationAgents, "AGENTS.md"},
		{IntegrationGeneric, ".mcp.json"},
		{IntegrationCursor, ".cursor"},
		{IntegrationVSCode, ".vscode"},
		{IntegrationOMP, ".omp"},
		{IntegrationFactory, ".factory"},
		{IntegrationOpenCode, "opencode.json"},
		{IntegrationNeovim, ".nvim"},
		{IntegrationHelix, ".helix"},
	}
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		if _, err := os.Stat(filepath.Join(root, check.path)); err == nil {
			out = append(out, check.name)
		}
	}
	sort.Strings(out)
	return out
}

func ParseIntegrationSelection(value string, detected []string) ([]string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "auto" {
		return normalizeIntegrations(detected)
	}
	if value == "none" {
		return []string{}, nil
	}
	if value == "all" {
		return append([]string(nil), allIntegrations...), nil
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		switch parts[i] {
		case "mcp", "generic":
			parts[i] = IntegrationGeneric
		case "oh-my-pi":
			parts[i] = IntegrationOMP
		case "vs-code":
			parts[i] = IntegrationVSCode
		}
	}
	return normalizeIntegrations(parts)
}

func normalizeIntegrations(values []string) ([]string, error) {
	known := make(map[string]bool, len(allIntegrations))
	for _, name := range allIntegrations {
		known[name] = true
	}
	set := map[string]bool{}
	for _, name := range values {
		if name == "" {
			continue
		}
		if !known[name] {
			return nil, fmt.Errorf("unknown integration %q (choose %s)", name, strings.Join(allIntegrations, ", "))
		}
		set[name] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func BuildSetupPlan(root string, integrations []string) (SetupPlan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return SetupPlan{}, err
	}
	integrations, err = normalizeIntegrations(integrations)
	if err != nil {
		return SetupPlan{}, err
	}
	plan := SetupPlan{Root: root, Integrations: integrations, Actions: make([]SetupAction, 0, len(integrations))}
	for _, name := range integrations {
		action := SetupAction{Integration: name, Description: "merge Prowl integration without replacing existing settings"}
		switch name {
		case IntegrationAgents:
			action.Path = "AGENTS.md"
		case IntegrationGeneric:
			action.Path = ".mcp.json"
		case IntegrationCursor:
			action.Path = ".cursor/mcp.json"
		case IntegrationVSCode:
			action.Path = ".vscode/mcp.json"
		case IntegrationOMP:
			action.Path = ".omp/mcp.json"
		case IntegrationFactory:
			action.Path = ".factory/mcp.json"
		case IntegrationOpenCode:
			action.Path = "opencode.json"
		case IntegrationNeovim:
			action.Path = ".prowl/editor/nvim.lua"
		case IntegrationHelix:
			action.Path = ".helix/languages.toml"
		}
		plan.Actions = append(plan.Actions, action)
	}
	return plan, nil
}

func ApplySetupPlan(plan SetupPlan) error {
	type snapshot struct {
		path   string
		data   []byte
		mode   os.FileMode
		exists bool
	}
	snapshots := make([]snapshot, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		path := filepath.Join(plan.Root, filepath.FromSlash(action.Path))
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			snapshots = append(snapshots, snapshot{path: path})
			continue
		}
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", action.Path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", action.Path, err)
		}
		snapshots = append(snapshots, snapshot{path: path, data: data, mode: info.Mode(), exists: true})
	}
	rollback := func() {
		for _, before := range snapshots {
			if before.exists {
				_ = os.MkdirAll(filepath.Dir(before.path), 0o755)
				_ = os.WriteFile(before.path, before.data, before.mode.Perm())
			} else {
				_ = os.Remove(before.path)
			}
		}
	}
	for _, action := range plan.Actions {
		var err error
		switch action.Integration {
		case IntegrationAgents:
			err = ensureAgentsBlock(filepath.Join(plan.Root, "AGENTS.md"))
		case IntegrationGeneric:
			err = mergeMCPConfig(filepath.Join(plan.Root, ".mcp.json"), "mcpServers")
		case IntegrationCursor:
			err = mergeMCPConfig(filepath.Join(plan.Root, ".cursor", "mcp.json"), "mcpServers")
		case IntegrationVSCode:
			err = mergeMCPConfig(filepath.Join(plan.Root, ".vscode", "mcp.json"), "servers")
		case IntegrationOMP:
			err = mergeMCPConfig(filepath.Join(plan.Root, ".omp", "mcp.json"), "mcpServers")
		case IntegrationFactory:
			err = mergeMCPConfig(filepath.Join(plan.Root, ".factory", "mcp.json"), "mcpServers")
		case IntegrationOpenCode:
			err = mergeOpenCode(filepath.Join(plan.Root, "opencode.json"))
		case IntegrationNeovim:
			err = injectNeovim(plan.Root)
		case IntegrationHelix:
			err = injectHelix(plan.Root)
		default:
			err = fmt.Errorf("unknown integration %q", action.Integration)
		}
		if err != nil {
			rollback()
			return fmt.Errorf("apply %s: %w", action.Integration, err)
		}
	}
	if err := VerifySetupPlan(plan); err != nil {
		rollback()
		return err
	}
	return nil
}

func VerifySetupPlan(plan SetupPlan) error {
	for _, action := range plan.Actions {
		path := filepath.Join(plan.Root, filepath.FromSlash(action.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("verify %s: %w", action.Integration, err)
		}
		if !strings.Contains(string(data), "prowl-agent") && !strings.Contains(string(data), "prowl_agent") {
			return fmt.Errorf("verify %s: Prowl ownership marker missing from %s", action.Integration, action.Path)
		}
	}
	return nil
}

func RemoveIntegrations(root string, integrations []string) error {
	integrations, err := normalizeIntegrations(integrations)
	if err != nil {
		return err
	}
	for _, name := range integrations {
		switch name {
		case IntegrationAgents:
			err = removeAgentsBlock(filepath.Join(root, "AGENTS.md"))
		case IntegrationGeneric:
			err = removeMCPConfig(filepath.Join(root, ".mcp.json"), "mcpServers")
		case IntegrationCursor:
			err = removeMCPConfig(filepath.Join(root, ".cursor", "mcp.json"), "mcpServers")
		case IntegrationVSCode:
			err = removeMCPConfig(filepath.Join(root, ".vscode", "mcp.json"), "servers")
		case IntegrationOMP:
			err = removeMCPConfig(filepath.Join(root, ".omp", "mcp.json"), "mcpServers")
		case IntegrationFactory:
			err = removeMCPConfig(filepath.Join(root, ".factory", "mcp.json"), "mcpServers")
		case IntegrationOpenCode:
			err = removeOpenCode(filepath.Join(root, "opencode.json"))
		case IntegrationNeovim:
			err = removeOwnedFile(filepath.Join(root, ".prowl", "editor", "nvim.lua"), nvimConfig)
		case IntegrationHelix:
			err = removeOwnedFile(filepath.Join(root, ".helix", "languages.toml"), helixConfig())
		}
		if err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

func removeMCPConfig(path, key string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("refusing to edit invalid JSON: %w", err)
	}
	servers, _ := doc[key].(map[string]any)
	if servers == nil {
		return nil
	}
	if _, ok := servers["prowl-agent"]; !ok {
		return nil
	}
	delete(servers, "prowl-agent")
	doc[key] = servers
	return writeJSON(path, doc)
}

func removeOpenCode(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("refusing to edit invalid JSON: %w", err)
	}
	mcp, _ := doc["mcp"].(map[string]any)
	if mcp == nil {
		return nil
	}
	delete(mcp, "prowl-agent")
	doc["mcp"] = mcp
	return writeJSON(path, doc)
}

func writeJSON(path string, doc map[string]any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func removeAgentsBlock(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	start := strings.Index(content, agentsMarker)
	if start < 0 {
		return nil
	}
	endRel := strings.Index(content[start:], agentsEndMarker)
	if endRel < 0 {
		return fmt.Errorf("refusing to remove malformed Prowl block without closing marker")
	}
	end := start + endRel + len(agentsEndMarker)
	updated := strings.TrimSpace(content[:start] + content[end:])
	if updated != "" {
		updated += "\n"
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func removeOwnedFile(path, expected string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(data) != expected {
		return fmt.Errorf("refusing to remove modified file %s", path)
	}
	return os.Remove(path)
}

func injectNeovim(root string) error {
	dir := filepath.Join(root, ".prowl", "editor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "nvim.lua"), []byte(nvimConfig), 0o644)
}

func injectHelix(root string) error {
	path := filepath.Join(root, ".helix", "languages.toml")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(helixConfig()), 0o644)
}
