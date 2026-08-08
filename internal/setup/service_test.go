package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestSetupDetectAndPlanDoNotWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"other":{"command":"secret-token"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, root)
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	detect, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !contains(detect.Integrations, IntegrationGeneric) || strings.Contains(detect.ProjectConfigVersion, root) {
		t.Fatalf("unsafe detect result: %+v", detect)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationGeneric})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Hash == "" || plan.Actions[0].Path != ".mcp.json" || strings.Contains(plan.Hash, root) {
		t.Fatalf("unsafe plan: %+v", plan)
	}
	if after := treeState(t, root); after != before {
		t.Fatalf("detect/plan wrote files:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestSetupApplyRequiresApprovalAndFreshPlanWithoutWrites(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	before := treeState(t, root)
	if _, err := service.Apply(context.Background(), ApplyRequest{Integrations: plan.Integrations, PlanHash: plan.Hash, ExpectedProjectConfigVersion: plan.ProjectConfigVersion, IdempotencyKey: "approval-denied"}); err == nil {
		t.Fatal("unapproved apply succeeded")
	}
	if after := treeState(t, root); after != before {
		t.Fatalf("denied apply wrote files: %s", after)
	}
	if err := os.MkdirAll(filepath.Join(root, ".prowl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prowl", "config.toml"), []byte("changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = treeState(t, root)
	if _, err := service.Apply(context.Background(), ApplyRequest{Integrations: plan.Integrations, PlanHash: plan.Hash, ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "stale-version"}); err == nil {
		t.Fatal("stale apply succeeded")
	}
	if after := treeState(t, root); after != before {
		t.Fatalf("stale apply wrote files: %s", after)
	}
}

func TestSetupApplyIdempotencyConflictAndAuditRedaction(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationGeneric})
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{Integrations: plan.Integrations, PlanHash: plan.Hash, ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "stable-key"}
	first, err := service.Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !first.Verified || len(first.RollbackManifest) != 1 {
		t.Fatalf("replay outcome = %+v, want durable verified original", second)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{Integrations: []string{IntegrationAgents}, PlanHash: plan.Hash, ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: request.IdempotencyKey}); err == nil {
		t.Fatal("mismatched replay succeeded")
	}
	for _, value := range append([]string{root, "secret-token"}, flattenOutcome(first)...) {
		if strings.Contains(strings.Join(flattenOutcome(first), "\n"), root) || strings.Contains(strings.Join(flattenOutcome(first), "\n"), "secret-token") {
			t.Fatalf("unsafe outcome value %q: %+v", value, first)
		}
	}
}

func TestSetupApplyReplaysCompletedRequestAfterConfigChanges(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "replay-after-config-change",
	}
	first, err := service.Apply(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prowl", "config.toml"), []byte("changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := service.Apply(context.Background(), request)
	if err != nil {
		t.Fatalf("completed replay was rejected after config change: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("completed replay changed: first=%+v second=%+v", first, second)
	}
}

func TestSetupRollbackPreservesFilesAfterMutationFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{ invalid json")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents, IntegrationCursor})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), ApplyRequest{Integrations: plan.Integrations, PlanHash: plan.Hash, ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "rollback"})
	if err == nil {
		t.Fatal("invalid configuration apply succeeded")
	}
	if data, readErr := os.ReadFile(path); readErr != nil || string(data) != string(original) {
		t.Fatalf("config after rollback = %q, %v; want %q", data, readErr, original)
	}
	if _, statErr := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("created file survived failed apply: %v", statErr)
	}
}

func TestSetupApplyRejectsSymlinkedDestinationWithoutWrites(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.md")
	original := []byte("do not modify\n")
	if err := os.WriteFile(victim, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "symlink",
	})
	if err == nil {
		t.Fatal("symlinked destination apply succeeded")
	}
	if got, readErr := os.ReadFile(victim); readErr != nil || string(got) != string(original) {
		t.Fatalf("symlink target changed: %q, %v", got, readErr)
	}
}

func TestSetupRollbackRemovesTransactionCreatedDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("{ invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationCursor, IntegrationGeneric})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "directory-rollback",
	})
	if err == nil {
		t.Fatal("invalid configuration apply succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".cursor")); !os.IsNotExist(statErr) {
		t.Fatalf("transaction-created integration directory survived rollback: %v", statErr)
	}
}

func TestSetupApplyRecoversInterruptedTransaction(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "interrupted",
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsBlock+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".prowl"), 0o755); err != nil {
		t.Fatal(err)
	}
	transaction := map[string]any{
		"schema_version": 1,
		"request":        request,
		"snapshots": []map[string]any{{
			"path": "AGENTS.md", "data": "", "mode": 0, "existed": false,
		}},
		"directories": []map[string]any{{
			"path": ".prowl", "created": true,
		}},
	}
	data, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prowl-setup-transaction.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Apply(context.Background(), request); err == nil {
		t.Fatal("interrupted transaction was applied instead of recovered")
	}
	if _, statErr := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("interrupted integration was not restored: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".prowl-setup-transaction.json")); !os.IsNotExist(statErr) {
		t.Fatalf("recovered transaction journal remained: %v", statErr)
	}
}

func TestSetupApplyRecoversLegacyInterruptedTransaction(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "legacy-interrupted",
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsBlock+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursor", "mcp.json"), []byte(`{"mcpServers":{"prowl-agent":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".prowl"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"schema_version": 1,
		"request":        request,
		"snapshots": []map[string]any{
			{"path": "AGENTS.md", "data": "", "mode": 0, "existed": false},
			{"path": ".cursor/mcp.json", "data": "", "mode": 0, "existed": false},
		},
		"directories": []map[string]any{
			{"path": ".prowl", "existed": false},
			{"path": ".cursor", "existed": false},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(root, ".prowl", "setup-transaction.json")
	if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), request); err == nil {
		t.Fatal("legacy interrupted transaction was applied instead of recovered")
	}
	if _, statErr := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("legacy interrupted integration was not restored: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".cursor")); !os.IsNotExist(statErr) {
		t.Fatalf("legacy transaction-created directory remained: %v", statErr)
	}
	if _, statErr := os.Stat(legacyPath); !os.IsNotExist(statErr) {
		t.Fatalf("legacy transaction journal remained: %v", statErr)
	}
}

func TestSetupApplyDoesNotRecoverLiveLockedTransaction(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "live-transaction",
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agentsBlock+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transaction := map[string]any{
		"schema_version": 1,
		"request":        request,
		"snapshots": []map[string]any{{
			"path": "AGENTS.md", "data": "", "mode": 0, "existed": false,
		}},
	}
	data, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prowl-setup-transaction.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(filepath.Join(root, ".prowl-setup.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := service.Apply(ctx, request); err == nil {
		t.Fatal("live transaction was processed while its lock was held")
	}
	if _, statErr := os.Stat(filepath.Join(root, "AGENTS.md")); statErr != nil {
		t.Fatalf("live transaction was rolled back while locked: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".prowl-setup-transaction.json")); statErr != nil {
		t.Fatalf("live transaction journal was removed while locked: %v", statErr)
	}
}

func TestSetupRollbackRetainsJournalUntilDirectoryCleanupCompletes(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursor", "user-owned"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := map[string]any{
		"schema_version": transactionSchema,
		"snapshots": []map[string]any{{
			"path": "absent", "data": "", "mode": 0, "existed": false,
		}},
		"directories": []map[string]any{{
			"path": ".cursor", "created": true,
		}},
	}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, transactionPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, transactionPath), data, 0o600); err != nil {
		t.Fatal(err)
	}
	pending, found, err := service.loadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("transaction journal was not loaded")
	}
	if err := service.rollbackTransaction(pending); err == nil {
		t.Fatal("rollback unexpectedly removed a non-empty directory")
	}
	if _, statErr := os.Stat(filepath.Join(root, transactionPath)); statErr != nil {
		t.Fatalf("rollback journal disappeared before cleanup completed: %v", statErr)
	}
}

func TestSetupRollbackPreservesUnownedDirectory(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	pending := transaction{Directories: []transactionDirectory{{Path: ".cursor"}}}
	if err := service.rollbackTransaction(pending); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".cursor")); statErr != nil {
		t.Fatalf("unowned directory was removed: %v", statErr)
	}
}

func TestSetupVerifyRejectsMissingMarker(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Verify(context.Background(), plan); err == nil {
		t.Fatal("verify accepted missing marker")
	}
}

func TestSetupApplyInstallsAndRemovesEmbeddedSkills(t *testing.T) {
	root := t.TempDir()
	// A present harness config dir makes it a detected, installable target.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if detected := DetectIntegrations(root); !contains(detected, IntegrationClaude) {
		t.Fatalf("config dir did not surface the integration: %v", detected)
	}
	service, err := NewService(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(context.Background(), []string{IntegrationOMP, IntegrationClaude})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Apply(context.Background(), ApplyRequest{
		Integrations: plan.Integrations, PlanHash: plan.Hash,
		ExpectedProjectConfigVersion: plan.ProjectConfigVersion, Approved: true, IdempotencyKey: "skills",
	})
	if err != nil {
		t.Fatalf("apply installing skills failed: %v", err)
	}
	if !outcome.Verified {
		t.Fatal("apply installing skills was not verified")
	}
	const skill = "prowl-repo-exploration"
	want, ok := skillContent(skill)
	if !ok || want == "" {
		t.Fatalf("embedded skill %q missing", skill)
	}
	dirs := []string{".omp/skills", ".claude/skills"}
	for _, dir := range dirs {
		path := filepath.Join(root, filepath.FromSlash(dir), skill, "SKILL.md")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("installed skill %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("installed skill %s content = %q, want embedded body", path, got)
		}
	}
	// Removing the integrations takes their installed skill files back out.
	if err := service.removeIntegrations([]string{IntegrationOMP, IntegrationClaude}); err != nil {
		t.Fatalf("remove integrations: %v", err)
	}
	for _, dir := range dirs {
		path := filepath.Join(root, filepath.FromSlash(dir), skill, "SKILL.md")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("skill %s survived removal: %v", path, err)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func treeState(t *testing.T, root string) string {
	t.Helper()
	var entries []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel)+":"+string(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "\n")
}

func flattenOutcome(outcome ApplyOutcome) []string {
	values := []string{outcome.PlanHash, outcome.ProjectConfigVersion, outcome.IdempotencyKey}
	for _, item := range outcome.RollbackManifest {
		values = append(values, item.Path)
	}
	return values
}
