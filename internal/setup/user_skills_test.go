package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/skills"
)

// wantAsset is an independent test oracle for one installed user asset. Its
// checksum is derived here with crypto/sha256 -- never through the production
// digest helper -- so a bug in the installer's checksum path cannot hide.
type wantAsset struct {
	AssetID  string
	Dest     string
	Content  string
	Checksum string
}

func sha256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// wantUserAssets rebuilds the expected destination set independently from the
// embedded skill/native bundles, so the tests do not merely echo the installer.
func wantUserAssets(version string, clients []string) []wantAsset {
	roots := map[string]string{"claude": ".claude/skills/prowl", "omp": ".omp/agent"}
	var out []wantAsset
	for _, client := range clients {
		root := roots[client]
		for _, asset := range skills.Native(client) {
			content := strings.ReplaceAll(asset.Content, "{{VERSION}}", version)
			out = append(out, wantAsset{
				AssetID:  client + ":" + asset.Path,
				Dest:     root + "/" + asset.Path,
				Content:  content,
				Checksum: sha256Hex(content),
			})
		}
		for _, skill := range skills.All() {
			out = append(out, wantAsset{
				AssetID:  client + ":skills/" + skill.Name + "/SKILL.md",
				Dest:     root + "/skills/" + skill.Name + "/SKILL.md",
				Content:  skill.Content,
				Checksum: sha256Hex(skill.Content),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dest < out[j].Dest })
	return out
}

func newUserOpts(t *testing.T) UserInstallOptions {
	t.Helper()
	return UserInstallOptions{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		Version:  "9.9.9",
		Clients:  []string{"claude", "omp"},
	}
}

// manifestPath is the persisted state file location: StateDir is the XDG state
// base, so the manifest lives under <StateDir>/prowl-agent/.
func manifestPath(opts UserInstallOptions) string {
	return filepath.Join(opts.StateDir, "prowl-agent", "agent-assets.json")
}

func actionByAssetID(plan UserPlan, id string) (UserAction, bool) {
	for _, action := range plan.Actions {
		if action.AssetID == id {
			return action, true
		}
	}
	return UserAction{}, false
}

func conflictByAssetID(plan UserPlan, id string) (UserConflict, bool) {
	for _, conflict := range plan.Conflicts {
		if conflict.AssetID == id {
			return conflict, true
		}
	}
	return UserConflict{}, false
}

func mustPlan(t *testing.T, opts UserInstallOptions) UserPlan {
	t.Helper()
	plan, err := PlanUserSkills(opts)
	if err != nil {
		t.Fatalf("PlanUserSkills: %v", err)
	}
	return plan
}

func mustApply(t *testing.T, opts UserInstallOptions) {
	t.Helper()
	plan := mustPlan(t, opts)
	if _, err := ApplyUserSkills(opts, plan, true); err != nil {
		t.Fatalf("ApplyUserSkills: %v", err)
	}
}

func planDests(plan UserPlan) []string {
	var out []string
	for _, action := range plan.Actions {
		out = append(out, action.Destination)
	}
	return out
}

// TestUserSkillPlanClaudeDestinations proves the Claude package lands under
// ~/.claude/skills/prowl/: the native plugin tree plus every canonical portable
// skill copied into the plugin's own skills/ directory.
func TestUserSkillPlanClaudeDestinations(t *testing.T) {
	opts := newUserOpts(t)
	opts.Clients = []string{"claude"}
	plan := mustPlan(t, opts)

	want := map[string]bool{
		".claude/skills/prowl/.claude-plugin/plugin.json": false,
		".claude/skills/prowl/agents/code-scout.md":       false,
		".claude/skills/prowl/commands/search.md":         false,
		".claude/skills/prowl/hooks/hooks.json":           false,
	}
	for _, skill := range skills.All() {
		want[".claude/skills/prowl/skills/"+skill.Name+"/SKILL.md"] = false
	}
	for _, action := range plan.Actions {
		if _, ok := want[action.Destination]; ok {
			want[action.Destination] = true
		}
		if action.Client != "claude" {
			t.Errorf("unexpected client %q for %s", action.Client, action.Destination)
		}
	}
	for dest, seen := range want {
		if !seen {
			t.Errorf("plan missing Claude destination %s", dest)
		}
	}
}

// TestUserSkillPlanOMPDestinations proves OMP lands under ~/.omp/agent/: skills/,
// the native agent, and the extension.
func TestUserSkillPlanOMPDestinations(t *testing.T) {
	opts := newUserOpts(t)
	opts.Clients = []string{"omp"}
	plan := mustPlan(t, opts)

	want := map[string]bool{
		".omp/agent/agents/code-scout.md":        false,
		".omp/agent/extensions/prowl-routing.ts": false,
		".omp/agent/skills/code-search/SKILL.md": false,
	}
	for _, action := range plan.Actions {
		if _, ok := want[action.Destination]; ok {
			want[action.Destination] = true
		}
		if action.Client != "omp" {
			t.Errorf("unexpected client %q for %s", action.Client, action.Destination)
		}
	}
	for dest, seen := range want {
		if !seen {
			t.Errorf("plan missing OMP destination %s", dest)
		}
	}
}

// TestUserSkillPlanPreviewOrderingIsStable proves the previewed destinations are
// deterministically ordered and carry no absolute paths or file bodies.
func TestUserSkillPlanPreviewOrderingIsStable(t *testing.T) {
	opts := newUserOpts(t)
	plan := mustPlan(t, opts)

	var got []string
	for _, action := range plan.Actions {
		if action.Kind != UserActionInstall {
			t.Fatalf("fresh plan action %s is %q, want install", action.Destination, action.Kind)
		}
		got = append(got, action.Destination)
		if filepath.IsAbs(action.Destination) || strings.HasPrefix(action.Destination, "/") {
			t.Errorf("preview leaks an absolute path: %s", action.Destination)
		}
		if strings.Contains(action.Destination, opts.Home) {
			t.Errorf("preview leaks the user home: %s", action.Destination)
		}
	}
	var want []string
	for _, asset := range wantUserAssets(opts.Version, opts.Clients) {
		want = append(want, asset.Dest)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("preview ordering mismatch\n got=%v\nwant=%v", got, want)
	}
	// Stable across repeated planning.
	if again := strings.Join(planDests(mustPlan(t, opts)), "\n"); again != strings.Join(got, "\n") {
		t.Fatalf("preview ordering not stable across runs")
	}
}

// TestUserSkillPlanFirstInstallChecksums proves a first install produces install
// actions whose checksums equal the exact bytes that would be written.
func TestUserSkillPlanFirstInstallChecksums(t *testing.T) {
	opts := newUserOpts(t)
	plan := mustPlan(t, opts)
	if len(plan.Conflicts) != 0 {
		t.Fatalf("fresh install reported conflicts: %+v", plan.Conflicts)
	}
	for _, asset := range wantUserAssets(opts.Version, opts.Clients) {
		action, ok := actionByAssetID(plan, asset.AssetID)
		if !ok {
			t.Fatalf("no action for %s", asset.AssetID)
		}
		if action.Kind != UserActionInstall {
			t.Errorf("%s: kind %q, want install", asset.AssetID, action.Kind)
		}
		if action.Checksum != asset.Checksum {
			t.Errorf("%s: checksum %q, want %q", asset.AssetID, action.Checksum, asset.Checksum)
		}
	}
}

// TestUserSkillPlanAllCurrentIsNoOp proves a re-plan after a successful install
// reports every asset as unchanged.
func TestUserSkillPlanAllCurrentIsNoOp(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)
	plan := mustPlan(t, opts)
	if len(plan.Conflicts) != 0 {
		t.Fatalf("re-plan reported conflicts: %+v", plan.Conflicts)
	}
	for _, action := range plan.Actions {
		if action.Kind != UserActionUnchanged {
			t.Errorf("%s: kind %q, want unchanged", action.Destination, action.Kind)
		}
	}
}

// TestUserSkillPlanUpdatesRecordedBytes proves that a destination whose bytes
// still match the recorded checksum is planned as an update when the shipped
// bytes change (here via the package version bump in the Claude manifest).
func TestUserSkillPlanUpdatesRecordedBytes(t *testing.T) {
	opts := newUserOpts(t)
	opts.Version = "1.0.0"
	mustApply(t, opts)

	bumped := opts
	bumped.Version = "2.0.0"
	plan := mustPlan(t, bumped)

	manifestID := "claude:.claude-plugin/plugin.json"
	action, ok := actionByAssetID(plan, manifestID)
	if !ok {
		t.Fatalf("no action for %s", manifestID)
	}
	if action.Kind != UserActionUpdate {
		t.Fatalf("%s: kind %q, want update", manifestID, action.Kind)
	}
	if _, ok := conflictByAssetID(plan, manifestID); ok {
		t.Fatalf("%s reported as conflict, want update", manifestID)
	}
}

// TestUserSkillPlanConflictsOnLocalModification proves a Prowl-owned file the
// user edited becomes a conflict, never an overwrite.
func TestUserSkillPlanConflictsOnLocalModification(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)

	dest := filepath.Join(opts.Home, ".omp", "agent", "skills", "code-search", "SKILL.md")
	if err := os.WriteFile(dest, []byte("user edited this skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, opts)
	id := "omp:skills/code-search/SKILL.md"
	if _, ok := conflictByAssetID(plan, id); !ok {
		t.Fatalf("locally modified %s not reported as a conflict", id)
	}
	if action, ok := actionByAssetID(plan, id); ok {
		t.Fatalf("locally modified %s planned as %q instead of a conflict", id, action.Kind)
	}
}

// TestUserSkillPlanConflictsOnUnownedFile proves a pre-existing file with no
// ownership record is a conflict, not a silent overwrite.
func TestUserSkillPlanConflictsOnUnownedFile(t *testing.T) {
	opts := newUserOpts(t)
	dest := filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("someone else's command\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, opts)
	id := "claude:commands/search.md"
	if _, ok := conflictByAssetID(plan, id); !ok {
		t.Fatalf("unowned pre-existing %s not reported as a conflict", id)
	}
	if _, ok := actionByAssetID(plan, id); ok {
		t.Fatalf("unowned pre-existing %s planned as a write", id)
	}
	// Other destinations still install normally.
	if _, ok := actionByAssetID(plan, "claude:hooks/hooks.json"); !ok {
		t.Fatalf("unrelated destination lost its install action")
	}
}

// TestUserSkillPlanRemovesExactLegacyAndPreservesModified proves migration off
// the retired skill removes an exact embedded copy but never a user-edited one.
func TestUserSkillPlanRemovesExactLegacyAndPreservesModified(t *testing.T) {
	legacy, ok := skills.Legacy("prowl-repo-exploration")
	if !ok {
		t.Fatal("legacy skill fixture missing")
	}
	opts := newUserOpts(t)

	exact := filepath.Join(opts.Home, ".claude", "skills", "prowl", "skills", "prowl-repo-exploration", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(exact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exact, []byte(legacy.Content), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := filepath.Join(opts.Home, ".omp", "agent", "skills", "prowl-repo-exploration", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(modified), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modified, []byte(legacy.Content+"\nlocal tweak\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlan(t, opts)
	removeID := "claude:skills/prowl-repo-exploration/SKILL.md"
	action, ok := actionByAssetID(plan, removeID)
	if !ok || action.Kind != UserActionRemove {
		t.Fatalf("exact legacy copy not planned for removal: %+v", action)
	}
	preserveID := "omp:skills/prowl-repo-exploration/SKILL.md"
	if _, ok := conflictByAssetID(plan, preserveID); !ok {
		t.Fatalf("modified legacy copy not preserved as a conflict")
	}
	if _, ok := actionByAssetID(plan, preserveID); ok {
		t.Fatalf("modified legacy copy planned for removal")
	}

	// Applying actually deletes the exact copy and leaves the edited one.
	if _, err := ApplyUserSkills(opts, plan, true); err != nil {
		t.Fatalf("apply legacy removal: %v", err)
	}
	if _, err := os.Lstat(exact); !os.IsNotExist(err) {
		t.Fatalf("exact legacy copy survived removal: %v", err)
	}
	if _, err := os.Stat(modified); err != nil {
		t.Fatalf("edited legacy copy was destroyed: %v", err)
	}
}

// TestUserSkillPlanRejectsSymlinkAndIrregularDestinations proves Prowl refuses a
// destination that is a symlink or otherwise not a regular file.
func TestUserSkillPlanRejectsSymlinkAndIrregularDestinations(t *testing.T) {
	opts := newUserOpts(t)

	symDest := filepath.Join(opts.Home, ".claude", "skills", "prowl", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(symDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(opts.Home, "elsewhere"), symDest); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A directory sitting where a regular file is expected is also irregular.
	dirDest := filepath.Join(opts.Home, ".omp", "agent", "extensions", "prowl-routing.ts")
	if err := os.MkdirAll(dirDest, 0o755); err != nil {
		t.Fatal(err)
	}

	plan := mustPlan(t, opts)
	if _, ok := conflictByAssetID(plan, "claude:hooks/hooks.json"); !ok {
		t.Fatalf("symlink destination not rejected as a conflict")
	}
	if _, ok := actionByAssetID(plan, "claude:hooks/hooks.json"); ok {
		t.Fatalf("symlink destination planned for a write")
	}
	if _, ok := conflictByAssetID(plan, "omp:extensions/prowl-routing.ts"); !ok {
		t.Fatalf("non-regular destination not rejected as a conflict")
	}
}

// TestUserSkillPlanSubstitutesPackageVersion proves the Claude manifest is
// stamped with the package version -- both in the checksum and in the bytes
// apply writes -- while the raw template token never reaches disk.
func TestUserSkillPlanSubstitutesPackageVersion(t *testing.T) {
	opts := newUserOpts(t)
	opts.Version = "3.4.5"
	plan := mustPlan(t, opts)

	manifestID := "claude:.claude-plugin/plugin.json"
	action, ok := actionByAssetID(plan, manifestID)
	if !ok {
		t.Fatalf("no action for %s", manifestID)
	}
	var stamped string
	for _, asset := range wantUserAssets(opts.Version, opts.Clients) {
		if asset.AssetID == manifestID {
			stamped = asset.Content
			if action.Checksum != asset.Checksum {
				t.Fatalf("manifest checksum %q, want stamped %q", action.Checksum, asset.Checksum)
			}
		}
	}
	if !strings.Contains(stamped, "3.4.5") || strings.Contains(stamped, "{{VERSION}}") {
		t.Fatalf("oracle content not stamped: %q", stamped)
	}

	if _, err := ApplyUserSkills(opts, plan, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(opts.Home, ".claude", "skills", "prowl", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "3.4.5") || strings.Contains(string(got), "{{VERSION}}") {
		t.Fatalf("written manifest not version-stamped: %s", got)
	}
}

// TestUserIntegrationApplyRequiresApprovalWritesNothing proves an unapproved
// apply is a strict no-op: no assets, no manifest.
func TestUserIntegrationApplyRequiresApprovalWritesNothing(t *testing.T) {
	opts := newUserOpts(t)
	plan := mustPlan(t, opts)
	before := treeState(t, opts.Home)

	if _, err := ApplyUserSkills(opts, plan, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("unapproved apply error = %v, want ErrApprovalRequired", err)
	}
	if after := treeState(t, opts.Home); after != before {
		t.Fatalf("unapproved apply wrote assets:\n%s", after)
	}
	if _, err := os.Stat(manifestPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("unapproved apply wrote a manifest: %v", err)
	}
}

// TestUserIntegrationApplyIsIdempotent proves re-applying an unchanged plan
// changes nothing on disk.
func TestUserIntegrationApplyIsIdempotent(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)

	before := treeState(t, opts.Home)
	stateBefore, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, opts)
	if _, err := ApplyUserSkills(opts, plan, true); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if after := treeState(t, opts.Home); after != before {
		t.Fatalf("idempotent apply changed assets")
	}
	stateAfter, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("idempotent apply changed the manifest")
	}
}

// TestUserIntegrationApplyRejectsStalePlan proves a plan is refused when a
// destination changed between planning and applying, and writes nothing.
func TestUserIntegrationApplyRejectsStalePlan(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)

	plan := mustPlan(t, opts) // all-unchanged plan

	dest := filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")
	if err := os.WriteFile(dest, []byte("changed after planning\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, opts.Home)
	if _, err := ApplyUserSkills(opts, plan, true); !errors.Is(err, ErrUserPlanStale) {
		t.Fatalf("stale apply error = %v, want ErrUserPlanStale", err)
	}
	if after := treeState(t, opts.Home); after != before {
		t.Fatalf("stale apply mutated the tree")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "changed after planning\n" {
		t.Fatalf("stale apply overwrote the changed destination")
	}
}

// TestUserIntegrationRollbackRemovesEarlierWritesAndDefersManifest proves a
// mid-apply write failure restores every earlier file and never leaves a
// manifest behind -- the manifest is written strictly last. The write seam is a
// private argument to the unexported apply, never a production interface.
func TestUserIntegrationRollbackRemovesEarlierWritesAndDefersManifest(t *testing.T) {
	opts := newUserOpts(t)
	plan := mustPlan(t, opts)

	asset := 0
	fail := func(root *os.Root, rel string, data []byte, mode os.FileMode) error {
		if strings.HasSuffix(rel, "agent-assets.json") {
			t.Errorf("manifest written before all assets succeeded")
			return nil
		}
		asset++
		if asset == 2 {
			return errors.New("injected asset write failure")
		}
		return writeAtomicInRoot(root, rel, data, mode)
	}

	if _, err := applyUserSkills(opts, plan, true, fail); err == nil {
		t.Fatal("apply succeeded despite injected failure")
	}
	if state := treeState(t, opts.Home); state != "" {
		t.Fatalf("rollback left files behind:\n%s", state)
	}
	if _, err := os.Stat(manifestPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("failed apply left a manifest: %v", err)
	}

	// A clean retry still succeeds, proving no corruption.
	mustApply(t, opts)
	if _, err := os.Stat(filepath.Join(opts.Home, ".claude", "skills", "prowl", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("clean retry did not install: %v", err)
	}
}

// TestUserIntegrationRollbackRestoresPriorFilesAndManifest proves a failure on
// the final manifest write restores every asset changed in that run and the
// prior manifest bytes.
func TestUserIntegrationRollbackRestoresPriorFilesAndManifest(t *testing.T) {
	opts := newUserOpts(t)
	opts.Version = "1.0.0"
	mustApply(t, opts)

	priorState, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(opts.Home, ".claude", "skills", "prowl", ".claude-plugin", "plugin.json")
	priorPlugin, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	// Delete a skill so the next apply has two writes before the manifest.
	deleted := filepath.Join(opts.Home, ".omp", "agent", "skills", "code-search", "SKILL.md")
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}

	bumped := opts
	bumped.Version = "2.0.0"
	plan := mustPlan(t, bumped)

	fail := func(root *os.Root, rel string, data []byte, mode os.FileMode) error {
		if strings.HasSuffix(rel, "agent-assets.json") {
			return errors.New("injected manifest write failure")
		}
		return writeAtomicInRoot(root, rel, data, mode)
	}

	if _, err := applyUserSkills(bumped, plan, true, fail); err == nil {
		t.Fatal("apply succeeded despite injected manifest failure")
	}

	gotPlugin, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPlugin) != string(priorPlugin) {
		t.Fatalf("plugin.json not restored to prior (v1) bytes after rollback")
	}
	if _, err := os.Stat(deleted); !os.IsNotExist(err) {
		t.Fatalf("reinstalled skill not rolled back to absent: %v", err)
	}
	gotState, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotState) != string(priorState) {
		t.Fatalf("prior manifest not restored after rollback")
	}
}

// TestUserIntegrationManifestRecordsOwnership proves the persisted manifest
// records schema, package version, asset id, destination, and installed-byte
// checksum for each owned asset.
func TestUserIntegrationManifestRecordsOwnership(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)

	data, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema         int    `json:"schema"`
		PackageVersion string `json:"package_version"`
		Assets         []struct {
			AssetID     string `json:"asset_id"`
			Destination string `json:"destination"`
			Checksum    string `json:"checksum"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if doc.Schema == 0 {
		t.Errorf("manifest schema not recorded")
	}
	if doc.PackageVersion != opts.Version {
		t.Errorf("manifest package version = %q, want %q", doc.PackageVersion, opts.Version)
	}
	byID := map[string]string{}
	for _, entry := range doc.Assets {
		if entry.AssetID == "" || entry.Destination == "" || entry.Checksum == "" {
			t.Errorf("incomplete manifest entry: %+v", entry)
		}
		byID[entry.AssetID] = entry.Checksum
	}
	for _, asset := range wantUserAssets(opts.Version, opts.Clients) {
		if byID[asset.AssetID] != asset.Checksum {
			t.Errorf("%s: manifest checksum %q, want %q", asset.AssetID, byID[asset.AssetID], asset.Checksum)
		}
	}
}

// TestUserIntegrationStateHasNoBodiesOrForeignPaths proves the manifest never
// stores file contents or absolute paths outside the user's integration roots.
func TestUserIntegrationStateHasNoBodiesOrForeignPaths(t *testing.T) {
	opts := newUserOpts(t)
	mustApply(t, opts)

	data, err := os.ReadFile(manifestPath(opts))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, opts.Home) || strings.Contains(text, opts.StateDir) {
		t.Fatalf("state leaks an absolute filesystem path")
	}
	// A distinctive slice of the Claude manifest body must not appear.
	if strings.Contains(text, "Route repository discovery through the read-only") {
		t.Fatalf("state stores installed file contents")
	}
	var doc struct {
		Assets []struct {
			Destination string `json:"destination"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, entry := range doc.Assets {
		if filepath.IsAbs(entry.Destination) || strings.HasPrefix(entry.Destination, "/") {
			t.Errorf("manifest destination is absolute: %s", entry.Destination)
		}
		if !strings.HasPrefix(entry.Destination, ".claude/") && !strings.HasPrefix(entry.Destination, ".omp/") {
			t.Errorf("manifest destination escapes the integration roots: %s", entry.Destination)
		}
	}
}

// TestUserSkillVerifyReportsHealthStates proves VerifyUserSkills exposes a
// single-model health view Task 6 can reuse: missing, current, stale, conflict.
func TestUserSkillVerifyReportsHealthStates(t *testing.T) {
	opts := newUserOpts(t)
	opts.Version = "1.0.0"

	fresh, err := VerifyUserSkills(opts)
	if err != nil {
		t.Fatalf("verify (fresh): %v", err)
	}
	for _, asset := range fresh.Assets {
		if asset.State != UserAssetMissing {
			t.Errorf("fresh %s state %q, want missing", asset.Destination, asset.State)
		}
	}

	mustApply(t, opts)
	current, err := VerifyUserSkills(opts)
	if err != nil {
		t.Fatalf("verify (installed): %v", err)
	}
	if !allState(current, UserAssetCurrent) {
		t.Fatalf("installed assets not all current: %+v", current.Assets)
	}

	// Local edit -> conflict for that asset.
	edited := filepath.Join(opts.Home, ".claude", "skills", "prowl", "commands", "search.md")
	if err := os.WriteFile(edited, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Version bump -> the manifest asset goes stale.
	bumped := opts
	bumped.Version = "2.0.0"
	health, err := VerifyUserSkills(bumped)
	if err != nil {
		t.Fatalf("verify (mixed): %v", err)
	}
	states := map[string]UserAssetState{}
	for _, asset := range health.Assets {
		states[asset.AssetID] = asset.State
	}
	if states["claude:commands/search.md"] != UserAssetConflict {
		t.Errorf("edited asset state %q, want conflict", states["claude:commands/search.md"])
	}
	if states["claude:.claude-plugin/plugin.json"] != UserAssetStale {
		t.Errorf("version-bumped manifest state %q, want stale", states["claude:.claude-plugin/plugin.json"])
	}
}

func allState(health UserHealth, want UserAssetState) bool {
	for _, asset := range health.Assets {
		if asset.State != want {
			return false
		}
	}
	return len(health.Assets) > 0
}
