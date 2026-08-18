package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/skills"
)

// This file owns Prowl's user-level agent-asset installer: the ownership-safe
// domain that lands the release-matched Claude and OMP bundles under the user's
// own configuration roots. It is deliberately concrete -- explicit options,
// plan, action, conflict, and manifest values -- rather than a generic provider
// framework, and it reuses the project installer's os.Root confinement, safe
// relative-path validation, atomic writes, and content digest.
//
// Roots:
//   - Claude: ~/.claude/skills/prowl/  (native plugin tree + canonical skills)
//   - OMP:    ~/.omp/agent/            (canonical skills, native agent, extension)
//
// The ownership manifest lives in the platform user-state directory,
// $XDG_STATE_HOME/prowl-agent/agent-assets.json (falling back to
// ~/.local/state/prowl-agent/agent-assets.json), records only metadata -- never
// file bodies or absolute paths outside the integration roots -- and is written
// last with an atomic replacement.

const (
	userStateFile      = "agent-assets.json"
	userManifestSchema = 1
	// userVersionToken is stamped with the package version wherever it appears
	// in an asset body (only the Claude plugin manifest carries it).
	userVersionToken = "{{VERSION}}"
)

var (
	// ErrUserPlanStale reports that a destination changed between planning and
	// applying, so the reviewed plan no longer describes the filesystem.
	ErrUserPlanStale = errors.New("user install plan is stale; re-plan against current state")

	errUserHomeRequired   = errors.New("user install requires a home directory")
	errUserDestinationDir = errors.New("user install destination parent is not a directory")
)

// UserInstallOptions configures one user-level install. Home and StateDir are
// explicit so tests use temporary directories and production supplies the real
// user directories; Version stamps version-templated assets; Clients are the
// detected harnesses (DetectInstalledHarnesses in production, explicit in tests).
type UserInstallOptions struct {
	Home     string
	StateDir string
	Version  string
	Clients  []string
}

// UserActionKind classifies a planned destination change.
type UserActionKind string

const (
	UserActionInstall   UserActionKind = "install"
	UserActionUpdate    UserActionKind = "update"
	UserActionRemove    UserActionKind = "remove"
	UserActionUnchanged UserActionKind = "unchanged"
)

// UserAction is one safe, planned change to a user-level asset destination.
// Destination is home-relative (forward-slash) so a preview never leaks the
// user's home path or file bodies. Checksum is the digest of the exact bytes
// the action would write (empty for a removal).
type UserAction struct {
	Kind        UserActionKind `json:"kind"`
	Client      string         `json:"client"`
	AssetID     string         `json:"asset_id"`
	Destination string         `json:"destination"`
	Checksum    string         `json:"checksum,omitempty"`
}

// UserConflict is a destination Prowl refuses to touch, with a home-relative
// reason. A conflict is never written or removed.
type UserConflict struct {
	Client      string `json:"client"`
	AssetID     string `json:"asset_id"`
	Destination string `json:"destination"`
	Reason      string `json:"reason"`
}

// UserPlan is a deterministic, reviewable user-install preview. It carries no
// file bodies and no absolute paths. Digest binds the reviewed plan to the
// filesystem state it was computed against.
type UserPlan struct {
	Version   string         `json:"version"`
	Actions   []UserAction   `json:"actions"`
	Conflicts []UserConflict `json:"conflicts"`
	Digest    string         `json:"digest"`
}

// UserApplyResult reports the actions an approved apply carried out.
type UserApplyResult struct {
	Version string       `json:"version"`
	Actions []UserAction `json:"actions"`
}

// userManifestEntry records ownership of one installed asset: its identifier,
// its owning client, its home-relative destination, and the checksum of the
// exact bytes Prowl wrote. It never holds a file body.
type userManifestEntry struct {
	AssetID     string `json:"asset_id"`
	Client      string `json:"client"`
	Destination string `json:"destination"`
	Checksum    string `json:"checksum"`
}

// userManifest is the persisted ownership record.
type userManifest struct {
	Schema         int                 `json:"schema"`
	PackageVersion string              `json:"package_version"`
	Assets         []userManifestEntry `json:"assets"`
}

// UserAssetState is a single-model health classification reused by Task 6's
// doctor: it never invents a second ownership view, it maps the plan.
type UserAssetState string

const (
	UserAssetMissing  UserAssetState = "missing"
	UserAssetCurrent  UserAssetState = "current"
	UserAssetStale    UserAssetState = "stale"
	UserAssetConflict UserAssetState = "conflict"
)

// UserAssetHealth is one asset's integration-health line.
type UserAssetHealth struct {
	Client      string         `json:"client"`
	AssetID     string         `json:"asset_id"`
	Destination string         `json:"destination"`
	State       UserAssetState `json:"state"`
}

// UserHealth is the verification primitive Task 6 renders in human and
// machine-readable formats.
type UserHealth struct {
	Version string            `json:"version"`
	Assets  []UserAssetHealth `json:"assets"`
}

// userWriter is the file-writing seam. Production is writeAtomicInRoot; tests
// pass an injected function to force a mid-apply failure and exercise rollback,
// so no production fault-injection interface or mutable package global exists.
type userWriter func(root *os.Root, rel string, data []byte, mode os.FileMode) error

// userCandidate is one desired destination the planner classifies against the
// filesystem: a copied asset, or a retired-skill removal candidate (legacy).
type userCandidate struct {
	client   string
	assetID  string
	dest     string
	content  string
	checksum string
	legacy   bool
}

func (candidate userCandidate) action(kind UserActionKind, checksum string) UserAction {
	return UserAction{Kind: kind, Client: candidate.client, AssetID: candidate.assetID, Destination: candidate.dest, Checksum: checksum}
}

func (candidate userCandidate) conflict(reason string) UserConflict {
	return UserConflict{Client: candidate.client, AssetID: candidate.assetID, Destination: candidate.dest, Reason: reason}
}

// destKind is the observed shape of a destination on disk.
type destKind int

const (
	destMissing destKind = iota
	destRegular
	destSymlink
	destIrregular
	destError
)

// normalizeUserClients keeps only the supported clients, deduplicated and
// sorted, so planning is deterministic regardless of detection order.
func normalizeUserClients(clients []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, client := range clients {
		switch client {
		case IntegrationClaude, IntegrationOMP:
			if !seen[client] {
				seen[client] = true
				out = append(out, client)
			}
		}
	}
	sort.Strings(out)
	return out
}

// userClientRoot is the home-relative root each client's assets install under.
func userClientRoot(client string) string {
	switch client {
	case IntegrationClaude:
		return ".claude/skills/prowl"
	case IntegrationOMP:
		return ".omp/agent"
	}
	return ""
}

// userStateDir resolves the platform user-state directory holding the ownership
// manifest. StateDir is the XDG state base override; otherwise $XDG_STATE_HOME,
// otherwise ~/.local/state. The manifest always lives under a prowl-agent/ leaf.
func userStateDir(opts UserInstallOptions) string {
	base := opts.StateDir
	if base == "" {
		base = os.Getenv("XDG_STATE_HOME")
	}
	if base == "" {
		base = filepath.Join(opts.Home, ".local", "state")
	}
	return filepath.Join(base, "prowl-agent")
}

// buildUserCandidates enumerates every desired destination for the detected
// clients -- native assets (version-stamped), canonical skills, and retired
// skill removals -- sorted by destination so previews and plans are stable.
func buildUserCandidates(opts UserInstallOptions) []userCandidate {
	var out []userCandidate
	for _, client := range normalizeUserClients(opts.Clients) {
		root := userClientRoot(client)
		for _, asset := range skills.Native(client) {
			content := strings.ReplaceAll(asset.Content, userVersionToken, opts.Version)
			out = append(out, userCandidate{
				client:   client,
				assetID:  client + ":" + asset.Path,
				dest:     root + "/" + asset.Path,
				content:  content,
				checksum: digest([]byte(content)),
			})
		}
		for _, skill := range skills.All() {
			rel := "skills/" + skill.Name + "/SKILL.md"
			out = append(out, userCandidate{
				client:   client,
				assetID:  client + ":" + rel,
				dest:     root + "/" + rel,
				content:  skill.Content,
				checksum: digest([]byte(skill.Content)),
			})
		}
		for _, name := range legacySkillNames {
			legacy, ok := skills.Legacy(name)
			if !ok {
				continue
			}
			rel := "skills/" + name + "/SKILL.md"
			out = append(out, userCandidate{
				client:  client,
				assetID: client + ":" + rel,
				dest:    root + "/" + rel,
				content: legacy.Content,
				legacy:  true,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dest < out[j].dest })
	return out
}

// readUserDest reports the on-disk shape and, for a regular file, the bytes of a
// destination, refusing to read through a symlinked path component.
func readUserDest(root *os.Root, dest string) ([]byte, destKind, error) {
	clean, err := validateRootPath(root, dest)
	if err != nil {
		if errors.Is(err, errSymlinkDestination) {
			return nil, destSymlink, nil
		}
		return nil, destError, err
	}
	info, err := root.Lstat(clean)
	if os.IsNotExist(err) {
		return nil, destMissing, nil
	}
	if err != nil {
		return nil, destError, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, destSymlink, nil
	}
	if !info.Mode().IsRegular() {
		return nil, destIrregular, nil
	}
	data, err := root.ReadFile(clean)
	if err != nil {
		return nil, destError, err
	}
	return data, destRegular, nil
}

// PlanUserSkills computes a deterministic, filesystem-only preview. The only
// writable conditions are: a missing destination (install); a present file
// whose bytes match the checksum Prowl previously recorded for that asset
// (update); and an exact embedded legacy body (removal). Every other condition
// -- a missing ownership record, a checksum mismatch, an unexpected file type,
// or a symlink -- is a conflict, never a write.
func PlanUserSkills(opts UserInstallOptions) (UserPlan, error) {
	if opts.Home == "" {
		return UserPlan{}, errUserHomeRequired
	}
	stored, err := loadUserManifest(opts)
	if err != nil {
		return UserPlan{}, err
	}
	records := make(map[string]userManifestEntry, len(stored.Assets))
	for _, entry := range stored.Assets {
		records[entry.AssetID] = entry
	}
	root, err := os.OpenRoot(opts.Home)
	if err != nil {
		return UserPlan{}, err
	}
	defer root.Close()

	var actions []UserAction
	var conflicts []UserConflict
	for _, candidate := range buildUserCandidates(opts) {
		data, kind, err := readUserDest(root, candidate.dest)
		if err != nil {
			return UserPlan{}, err
		}
		if candidate.legacy {
			switch kind {
			case destMissing:
				// Nothing to migrate off the install path.
			case destSymlink:
				conflicts = append(conflicts, candidate.conflict("retired skill path is a symbolic link; left in place"))
			case destIrregular:
				conflicts = append(conflicts, candidate.conflict("retired skill path is not a regular file; left in place"))
			case destRegular:
				if string(data) == candidate.content {
					actions = append(actions, candidate.action(UserActionRemove, ""))
				} else {
					conflicts = append(conflicts, candidate.conflict("retired skill modified locally; left in place"))
				}
			}
			continue
		}
		switch kind {
		case destMissing:
			actions = append(actions, candidate.action(UserActionInstall, candidate.checksum))
		case destSymlink:
			conflicts = append(conflicts, candidate.conflict("destination is a symbolic link; Prowl does not write through symlinks"))
		case destIrregular:
			conflicts = append(conflicts, candidate.conflict("destination is not a regular file"))
		case destRegular:
			current := digest(data)
			switch {
			case current == candidate.checksum:
				actions = append(actions, candidate.action(UserActionUnchanged, candidate.checksum))
			default:
				record, owned := records[candidate.assetID]
				switch {
				case !owned:
					conflicts = append(conflicts, candidate.conflict("pre-existing file without a Prowl ownership record"))
				case current == record.Checksum:
					actions = append(actions, candidate.action(UserActionUpdate, candidate.checksum))
				default:
					conflicts = append(conflicts, candidate.conflict("locally modified since Prowl installed it"))
				}
			}
		}
	}
	plan := UserPlan{Version: opts.Version, Actions: actions, Conflicts: conflicts}
	plan.Digest = userPlanDigest(plan)
	return plan, nil
}

// userPlanDigest fingerprints the reviewable content of a plan so apply can
// detect a stale plan and enforce a matching reviewed digest.
func userPlanDigest(plan UserPlan) string {
	canonical := struct {
		Version   string         `json:"version"`
		Actions   []UserAction   `json:"actions"`
		Conflicts []UserConflict `json:"conflicts"`
	}{plan.Version, plan.Actions, plan.Conflicts}
	data, _ := json.Marshal(canonical)
	return digest(data)
}

// ApplyUserSkills applies an approved, reviewed plan transactionally.
func ApplyUserSkills(opts UserInstallOptions, plan UserPlan, approved bool) (UserApplyResult, error) {
	return applyUserSkills(opts, plan, approved, writeAtomicInRoot)
}

// applyUserSkills is the transactional core. It requires explicit approval and a
// plan digest that still matches the filesystem, snapshots every touched file
// (including the ownership manifest) for in-memory rollback, applies the safe
// actions, and writes the manifest last with an atomic replacement. Any write
// failure restores every earlier file and the prior manifest. The write seam is
// a private argument so tests can force a failure without a production
// fault-injection interface.
func applyUserSkills(opts UserInstallOptions, plan UserPlan, approved bool, write userWriter) (UserApplyResult, error) {
	if !approved {
		return UserApplyResult{}, ErrApprovalRequired
	}
	fresh, err := PlanUserSkills(opts)
	if err != nil {
		return UserApplyResult{}, err
	}
	if fresh.Digest != plan.Digest {
		return UserApplyResult{}, ErrUserPlanStale
	}

	root, err := os.OpenRoot(opts.Home)
	if err != nil {
		return UserApplyResult{}, err
	}
	defer root.Close()

	stateDir := userStateDir(opts)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return UserApplyResult{}, err
	}
	stateRoot, err := os.OpenRoot(stateDir)
	if err != nil {
		return UserApplyResult{}, err
	}
	defer stateRoot.Close()

	prior, err := loadUserManifest(opts)
	if err != nil {
		return UserApplyResult{}, err
	}

	contents := make(map[string]string)
	for _, candidate := range buildUserCandidates(opts) {
		contents[candidate.assetID] = candidate.content
	}

	txn := &userTxn{}
	// Snapshot the manifest first so rollback restores the prior ownership even
	// if the very last write -- the manifest itself -- fails.
	manifestSnapshot, err := snapshotUserFile(stateRoot, userStateFile)
	if err != nil {
		return UserApplyResult{}, err
	}
	txn.files = append(txn.files, manifestSnapshot)

	for _, action := range fresh.Actions {
		switch action.Kind {
		case UserActionInstall, UserActionUpdate:
			if err := ensureUserDirs(root, action.Destination, txn); err != nil {
				return rollbackUser(txn, err)
			}
			snapshot, err := snapshotUserFile(root, action.Destination)
			if err != nil {
				return rollbackUser(txn, err)
			}
			txn.files = append(txn.files, snapshot)
			if err := write(root, action.Destination, []byte(contents[action.AssetID]), 0o644); err != nil {
				return rollbackUser(txn, err)
			}
		case UserActionRemove:
			snapshot, err := snapshotUserFile(root, action.Destination)
			if err != nil {
				return rollbackUser(txn, err)
			}
			txn.files = append(txn.files, snapshot)
			if err := removeOwnedFile(root, action.Destination, contents[action.AssetID]); err != nil {
				return rollbackUser(txn, err)
			}
		case UserActionUnchanged:
			// Already current; recorded in the manifest, never rewritten.
		}
	}

	data, err := marshalUserManifest(mergeUserManifest(prior, opts, fresh))
	if err != nil {
		return rollbackUser(txn, err)
	}
	if err := write(stateRoot, userStateFile, data, 0o644); err != nil {
		return rollbackUser(txn, err)
	}
	return UserApplyResult{Version: opts.Version, Actions: fresh.Actions}, nil
}

// mergeUserManifest folds the applied actions into the prior ownership record:
// installs/updates/unchanged assets are recorded with the exact installed
// checksum, removals drop their record, and untouched entries (including
// conflicts) are preserved.
func mergeUserManifest(prior userManifest, opts UserInstallOptions, plan UserPlan) userManifest {
	records := make(map[string]userManifestEntry, len(prior.Assets))
	for _, entry := range prior.Assets {
		records[entry.AssetID] = entry
	}
	for _, action := range plan.Actions {
		switch action.Kind {
		case UserActionInstall, UserActionUpdate, UserActionUnchanged:
			records[action.AssetID] = userManifestEntry{
				AssetID:     action.AssetID,
				Client:      action.Client,
				Destination: action.Destination,
				Checksum:    action.Checksum,
			}
		case UserActionRemove:
			delete(records, action.AssetID)
		}
	}
	out := userManifest{Schema: userManifestSchema, PackageVersion: opts.Version}
	for _, entry := range records {
		out.Assets = append(out.Assets, entry)
	}
	sort.Slice(out.Assets, func(i, j int) bool { return out.Assets[i].Destination < out.Assets[j].Destination })
	return out
}

func marshalUserManifest(m userManifest) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// loadUserManifest reads the ownership manifest, returning an empty manifest
// when the state directory or file does not yet exist.
func loadUserManifest(opts UserInstallOptions) (userManifest, error) {
	root, err := os.OpenRoot(userStateDir(opts))
	if err != nil {
		if os.IsNotExist(err) {
			return userManifest{}, nil
		}
		return userManifest{}, err
	}
	defer root.Close()
	data, err := root.ReadFile(userStateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return userManifest{}, nil
		}
		return userManifest{}, err
	}
	var m userManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return userManifest{}, err
	}
	return m, nil
}

// VerifyUserSkills reports per-asset integration health from the same plan the
// installer uses, so Task 6's doctor reads one ownership model, not a second.
func VerifyUserSkills(opts UserInstallOptions) (UserHealth, error) {
	plan, err := PlanUserSkills(opts)
	if err != nil {
		return UserHealth{}, err
	}
	var assets []UserAssetHealth
	for _, action := range plan.Actions {
		var state UserAssetState
		switch action.Kind {
		case UserActionInstall:
			state = UserAssetMissing
		case UserActionUpdate:
			state = UserAssetStale
		case UserActionUnchanged:
			state = UserAssetCurrent
		case UserActionRemove:
			// A retired-skill removal is migration, not a health signal.
			continue
		}
		assets = append(assets, UserAssetHealth{Client: action.Client, AssetID: action.AssetID, Destination: action.Destination, State: state})
	}
	for _, conflict := range plan.Conflicts {
		assets = append(assets, UserAssetHealth{Client: conflict.Client, AssetID: conflict.AssetID, Destination: conflict.Destination, State: UserAssetConflict})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Destination < assets[j].Destination })
	return UserHealth{Version: opts.Version, Assets: assets}, nil
}

// userFileSnapshot is one file's pre-apply state for in-memory rollback.
type userFileSnapshot struct {
	root    *os.Root
	rel     string
	existed bool
	data    []byte
	mode    os.FileMode
}

// userDir is a directory the apply created and rollback must remove.
type userDir struct {
	root *os.Root
	rel  string
}

// userTxn accumulates the reversals a failed apply must perform, entirely in
// memory -- there is no persisted journal at the user level.
type userTxn struct {
	files []userFileSnapshot
	dirs  []userDir
}

func (txn *userTxn) rollback() {
	for i := len(txn.files) - 1; i >= 0; i-- {
		snapshot := txn.files[i]
		if snapshot.existed {
			_ = writeAtomicInRoot(snapshot.root, snapshot.rel, snapshot.data, snapshot.mode)
			continue
		}
		_ = snapshot.root.Remove(snapshot.rel)
	}
	for i := len(txn.dirs) - 1; i >= 0; i-- {
		_ = txn.dirs[i].root.Remove(txn.dirs[i].rel)
	}
}

func rollbackUser(txn *userTxn, cause error) (UserApplyResult, error) {
	txn.rollback()
	return UserApplyResult{}, cause
}

// snapshotUserFile captures a destination's pre-apply bytes and mode.
func snapshotUserFile(root *os.Root, rel string) (userFileSnapshot, error) {
	clean, err := validateRootPath(root, rel)
	if err != nil {
		return userFileSnapshot{}, err
	}
	info, err := root.Stat(clean)
	if os.IsNotExist(err) {
		return userFileSnapshot{root: root, rel: clean}, nil
	}
	if err != nil {
		return userFileSnapshot{}, err
	}
	data, err := root.ReadFile(clean)
	if err != nil {
		return userFileSnapshot{}, err
	}
	return userFileSnapshot{root: root, rel: clean, existed: true, data: data, mode: info.Mode().Perm()}, nil
}

// ensureUserDirs creates the missing parent directories of rel, recording only
// the ones it creates so rollback removes exactly those and no pre-existing one.
func ensureUserDirs(root *os.Root, rel string, txn *userTxn) error {
	clean, err := validateRootPath(root, rel)
	if err != nil {
		return err
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(filepath.Dir(clean)), "/") {
		if component == "." || component == "" {
			continue
		}
		if current == "" {
			current = component
		} else {
			current = current + "/" + component
		}
		info, err := root.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errUserDestinationDir
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err := root.Mkdir(current, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		txn.dirs = append(txn.dirs, userDir{root: root, rel: current})
	}
	return nil
}
