package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	userStateLeaf      = "prowl-agent"
	userStateLock      = "prowl-agent.lock"
	userManifestSchema = 1
	// userVersionToken is stamped with the package version wherever it appears
	// in an asset body (only the Claude plugin manifest carries it).
	userVersionToken = "{{VERSION}}"
)

var (
	// ErrUserPlanStale reports that the reviewed plan no longer matches reality:
	// its own digest is inconsistent, or a destination changed between planning
	// and applying.
	ErrUserPlanStale = errors.New("user install plan is stale; re-plan against current state")

	errUserHomeRequired    = errors.New("user install requires a home directory")
	errUserDestinationDir  = errors.New("user install destination parent is not a directory")
	errUserManifestChanged = errors.New("ownership manifest changed since planning; aborting")
)

// UserInstallOptions configures one user-level install. Home and StateDir are
// explicit so tests use temporary directories and production supplies the real
// user directories; Version stamps version-templated assets; Clients are the
// detected harnesses (DetectInstalledHarnesses in production, explicit in tests).
// StateDir is the XDG state base override: the manifest lives beneath its
// prowl-agent/ leaf.
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
// file bodies and no absolute paths. Digest binds the reviewed plan to both its
// own content and the filesystem state it was computed against.
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

// userDecision is the classification of one candidate against the filesystem:
// an actionable kind, a conflict, or neither (a missing retired skill).
type userDecision struct {
	kind     UserActionKind
	checksum string
	conflict *UserConflict
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

// userStateBase resolves the XDG state base: the explicit StateDir override,
// then $XDG_STATE_HOME, then ~/.local/state.
func userStateBase(opts UserInstallOptions) string {
	if opts.StateDir != "" {
		return opts.StateDir
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(opts.Home, ".local", "state")
}

// userStateDir is the directory holding the ownership manifest.
func userStateDir(opts UserInstallOptions) string {
	return filepath.Join(userStateBase(opts), userStateLeaf)
}

// deepestExistingDir returns the deepest existing ancestor of path, rejecting a
// component that exists but is a symlink or not a directory. Non-existent
// components are created later, symlink-checked, inside an os.Root.
func deepestExistingDir(path string) (string, error) {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", fmt.Errorf("state path %s is not a directory", filepath.Base(path))
			}
			return path, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path, nil
		}
		path = parent
	}
}

// stateManifestLocation anchors the manifest at the deepest existing ancestor of
// the state directory and returns the manifest's path relative to that anchor.
// Rooting at the anchor and confining the generated components means Prowl never
// reads or writes through a symlinked state directory.
func stateManifestLocation(opts UserInstallOptions) (anchor string, rel string, err error) {
	stateDir := userStateDir(opts)
	anchor, err = deepestExistingDir(stateDir)
	if err != nil {
		return "", "", err
	}
	relDir, err := filepath.Rel(anchor, stateDir)
	if err != nil {
		return "", "", err
	}
	return anchor, filepath.ToSlash(filepath.Join(relDir, userStateFile)), nil
}

// acquireUserStateLock takes the persistent user-state lock that serializes
// concurrent applies. The lock file sits in the state base -- outside the
// transaction-owned prowl-agent leaf and matching the project setup-lock
// pattern -- so a failed apply can roll back a newly created prowl-agent
// directory without deleting the inode queued waiters may hold. Its containing
// directory is created persistently (never rolled back) and symlink-checked;
// the lock file itself is never removed. Lock and open errors are surfaced.
func acquireUserStateLock(opts UserInstallOptions) (func(), error) {
	base := userStateBase(opts)
	anchor, err := deepestExistingDir(base)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	relBase, err := filepath.Rel(anchor, base)
	if err != nil {
		root.Close()
		return nil, err
	}
	lockRel := filepath.ToSlash(filepath.Join(relBase, userStateLock))
	// Create the base directory chain persistently (a throwaway transaction is
	// discarded, so these directories are never rolled back), symlink-safe.
	if err := ensureUserDirs(root, lockRel, &userTxn{}); err != nil {
		root.Close()
		return nil, err
	}
	clean, err := validateRootPath(root, lockRel)
	if err != nil {
		root.Close()
		return nil, err
	}
	file, err := root.OpenFile(clean, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		root.Close()
		return nil, err
	}
	unlock, err := lockSetupFile(context.Background(), file, setupLockTimeout)
	if err != nil {
		root.Close()
		return nil, err
	}
	return func() {
		unlock()
		root.Close()
	}, nil
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

// lookupRecord returns the ownership record that authorizes a candidate only
// when AssetID, Client, and Destination all match. A record present under the
// same AssetID but with a different client or destination does not authorize a
// write -- it is a corrupt or foreign entry.
func lookupRecord(records map[string]userManifestEntry, candidate userCandidate) (userManifestEntry, bool) {
	record, ok := records[candidate.assetID]
	if !ok || record.Client != candidate.client || record.Destination != candidate.dest {
		return userManifestEntry{}, false
	}
	return record, true
}

// classifyUserDest is the single ownership-safety decision, shared by planning
// and the per-action recheck at mutation time. The only writable conditions
// are: a missing destination (install); a present file whose bytes match the
// checksum Prowl recorded for that exact asset (update); and an exact embedded
// legacy body (removal). Every other condition -- a missing or mismatched
// ownership record, a checksum mismatch, an unexpected file type, a symlink, or
// a byte-identical file Prowl never recorded -- is a conflict, never a write.
func classifyUserDest(root *os.Root, candidate userCandidate, records map[string]userManifestEntry) (userDecision, error) {
	data, kind, err := readUserDest(root, candidate.dest)
	if err != nil {
		return userDecision{}, err
	}
	if candidate.legacy {
		switch kind {
		case destMissing:
			return userDecision{}, nil
		case destSymlink:
			conflict := candidate.conflict("retired skill path is a symbolic link; left in place")
			return userDecision{conflict: &conflict}, nil
		case destIrregular:
			conflict := candidate.conflict("retired skill path is not a regular file; left in place")
			return userDecision{conflict: &conflict}, nil
		default:
			if string(data) == candidate.content {
				return userDecision{kind: UserActionRemove}, nil
			}
			conflict := candidate.conflict("retired skill modified locally; left in place")
			return userDecision{conflict: &conflict}, nil
		}
	}
	switch kind {
	case destMissing:
		return userDecision{kind: UserActionInstall, checksum: candidate.checksum}, nil
	case destSymlink:
		conflict := candidate.conflict("destination is a symbolic link; Prowl does not write through symlinks")
		return userDecision{conflict: &conflict}, nil
	case destIrregular:
		conflict := candidate.conflict("destination is not a regular file")
		return userDecision{conflict: &conflict}, nil
	default:
		current := digest(data)
		record, owned := lookupRecord(records, candidate)
		if !owned {
			conflict := candidate.conflict("pre-existing file without a Prowl ownership record")
			return userDecision{conflict: &conflict}, nil
		}
		switch {
		case current == candidate.checksum:
			return userDecision{kind: UserActionUnchanged, checksum: candidate.checksum}, nil
		case current == record.Checksum:
			return userDecision{kind: UserActionUpdate, checksum: candidate.checksum}, nil
		default:
			conflict := candidate.conflict("locally modified since Prowl installed it")
			return userDecision{conflict: &conflict}, nil
		}
	}
}

func recordIndex(m userManifest) map[string]userManifestEntry {
	records := make(map[string]userManifestEntry, len(m.Assets))
	for _, entry := range m.Assets {
		records[entry.AssetID] = entry
	}
	return records
}

// PlanUserSkills computes a deterministic, filesystem-only preview.
func PlanUserSkills(opts UserInstallOptions) (UserPlan, error) {
	if opts.Home == "" {
		return UserPlan{}, errUserHomeRequired
	}
	stored, err := loadUserManifest(opts)
	if err != nil {
		return UserPlan{}, err
	}
	records := recordIndex(stored)
	root, err := os.OpenRoot(opts.Home)
	if err != nil {
		return UserPlan{}, err
	}
	defer root.Close()

	var actions []UserAction
	var conflicts []UserConflict
	for _, candidate := range buildUserCandidates(opts) {
		decision, err := classifyUserDest(root, candidate, records)
		if err != nil {
			return UserPlan{}, err
		}
		switch {
		case decision.conflict != nil:
			conflicts = append(conflicts, *decision.conflict)
		case decision.kind != "":
			actions = append(actions, candidate.action(decision.kind, decision.checksum))
		}
	}
	plan := UserPlan{Version: opts.Version, Actions: actions, Conflicts: conflicts}
	plan.Digest = userPlanDigest(plan)
	return plan, nil
}

// userPlanDigest fingerprints the reviewable content of a plan so apply can
// verify the plan is internally consistent and detect a stale plan.
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

// applyUserSkills is the transactional core. It requires explicit approval, a
// plan whose digest matches its own content, and a plan digest that still
// matches the filesystem. It snapshots every touched file (including the
// ownership manifest) and every directory it creates (including the state
// directory) for in-memory rollback, rechecks each action's reviewed
// precondition at the mutation point, applies the safe actions, and writes the
// manifest last with an atomic replacement. Any failure restores every earlier
// file and the prior manifest; a rollback failure is combined with the
// initiating error. The write seam is a private argument so tests can force a
// failure without a production fault-injection interface.
func applyUserSkills(opts UserInstallOptions, plan UserPlan, approved bool, write userWriter) (UserApplyResult, error) {
	if !approved {
		return UserApplyResult{}, ErrApprovalRequired
	}
	if plan.Digest == "" || plan.Digest != userPlanDigest(plan) {
		return UserApplyResult{}, ErrUserPlanStale
	}

	// Serialize concurrent Prowl applies on a persistent user-state lock, held
	// from before fresh planning and manifest load through the per-action
	// rechecks, the manifest commit, and any rollback. Disjoint client installs
	// therefore cannot last-writer-win and drop each other's ownership records.
	unlock, err := acquireUserStateLock(opts)
	if err != nil {
		return UserApplyResult{}, err
	}
	defer unlock()

	fresh, err := PlanUserSkills(opts)
	if err != nil {
		return UserApplyResult{}, err
	}
	if fresh.Digest != plan.Digest {
		return UserApplyResult{}, ErrUserPlanStale
	}

	anchor, manifestRel, err := stateManifestLocation(opts)
	if err != nil {
		return UserApplyResult{}, err
	}
	root, err := os.OpenRoot(opts.Home)
	if err != nil {
		return UserApplyResult{}, err
	}
	defer root.Close()
	stateRoot, err := os.OpenRoot(anchor)
	if err != nil {
		return UserApplyResult{}, err
	}
	defer stateRoot.Close()

	prior, err := loadUserManifest(opts)
	if err != nil {
		return UserApplyResult{}, err
	}
	records := recordIndex(prior)

	byID := make(map[string]userCandidate)
	for _, candidate := range buildUserCandidates(opts) {
		byID[candidate.assetID] = candidate
	}

	txn := &userTxn{}
	// Create the state directory chain inside the transaction so a failed apply
	// removes only the directories it created and preserves pre-existing ones.
	if err := ensureUserDirs(stateRoot, manifestRel, txn); err != nil {
		return rollbackUser(txn, err)
	}
	// Snapshot the manifest first so rollback restores the prior ownership even
	// when the last write -- the manifest -- fails.
	manifestSnapshot, err := snapshotUserFile(stateRoot, manifestRel)
	if err != nil {
		return rollbackUser(txn, err)
	}
	txn.files = append(txn.files, manifestSnapshot)

	for _, action := range fresh.Actions {
		candidate := byID[action.AssetID]
		switch action.Kind {
		case UserActionInstall, UserActionUpdate:
			if err := recheckAction(root, candidate, records, action); err != nil {
				return rollbackUser(txn, err)
			}
			if err := ensureUserDirs(root, action.Destination, txn); err != nil {
				return rollbackUser(txn, err)
			}
			snapshot, err := snapshotUserFile(root, action.Destination)
			if err != nil {
				return rollbackUser(txn, err)
			}
			txn.files = append(txn.files, snapshot)
			if err := write(root, action.Destination, []byte(candidate.content), 0o644); err != nil {
				return rollbackUser(txn, err)
			}
		case UserActionRemove:
			if err := recheckAction(root, candidate, records, action); err != nil {
				return rollbackUser(txn, err)
			}
			snapshot, err := snapshotUserFile(root, action.Destination)
			if err != nil {
				return rollbackUser(txn, err)
			}
			txn.files = append(txn.files, snapshot)
			if err := removeOwnedFile(root, action.Destination, candidate.content); err != nil {
				return rollbackUser(txn, err)
			}
		case UserActionUnchanged:
			// Unchanged assets are finalized in a single pass below, immediately
			// before the manifest commit -- never in action order -- so drift
			// after their position but before commit is still caught.
		}
	}

	// Final pass: after every mutable action, revalidate all unchanged assets
	// immediately before the manifest snapshot check and commit. An unchanged
	// asset deleted or edited since the fresh plan aborts and rolls back rather
	// than being committed as owned.
	for _, action := range fresh.Actions {
		if action.Kind != UserActionUnchanged {
			continue
		}
		if err := recheckAction(root, byID[action.AssetID], records, action); err != nil {
			return rollbackUser(txn, err)
		}
	}

	// Manifest snapshot validation: reread the safely rooted manifest and
	// confirm it still matches the snapshot exactly. If an external or older
	// installer created, removed, replaced, symlinked, or changed it since the
	// snapshot, abort and roll back rather than overwriting stale ownership.
	if unchanged, err := manifestMatchesSnapshot(stateRoot, manifestRel, manifestSnapshot); err != nil {
		return rollbackUser(txn, err)
	} else if !unchanged {
		return rollbackUser(txn, errUserManifestChanged)
	}

	data, err := marshalUserManifest(mergeUserManifest(prior, opts, fresh))
	if err != nil {
		return rollbackUser(txn, err)
	}
	if err := write(stateRoot, manifestRel, data, 0o644); err != nil {
		return rollbackUser(txn, err)
	}
	return UserApplyResult{Version: opts.Version, Actions: fresh.Actions}, nil
}

// manifestMatchesSnapshot rereads the ownership manifest through the safely
// rooted state directory and reports whether it still matches the pre-apply
// snapshot exactly -- same existence, a regular file (not a symlink or
// directory), and identical bytes.
func manifestMatchesSnapshot(stateRoot *os.Root, rel string, snapshot userFileSnapshot) (bool, error) {
	data, kind, err := readUserDest(stateRoot, rel)
	if err != nil {
		return false, err
	}
	if !snapshot.existed {
		return kind == destMissing, nil
	}
	if kind != destRegular {
		return false, nil
	}
	return string(data) == string(snapshot.data), nil
}

// recheckAction re-derives the classification of a planned action's destination
// at the mutation point. If the destination changed since the fresh plan was
// computed -- so the reviewed precondition (install target still missing, update
// bytes still the owned checksum, removal still the exact legacy body) no longer
// holds -- it reports an error so apply aborts and rolls back instead of
// overwriting.
func recheckAction(root *os.Root, candidate userCandidate, records map[string]userManifestEntry, planned UserAction) error {
	decision, err := classifyUserDest(root, candidate, records)
	if err != nil {
		return err
	}
	if decision.conflict != nil || decision.kind != planned.Kind || decision.checksum != planned.Checksum {
		return fmt.Errorf("destination %s changed since planning", planned.Destination)
	}
	return nil
}

// mergeUserManifest folds the applied actions into the prior ownership record:
// installs/updates/unchanged assets are recorded with the exact installed
// checksum, removals drop their record, and untouched entries (including
// conflicts) are preserved.
func mergeUserManifest(prior userManifest, opts UserInstallOptions, plan UserPlan) userManifest {
	records := recordIndex(prior)
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
// when the state directory or file does not yet exist, and rejecting a
// symlinked state path.
func loadUserManifest(opts UserInstallOptions) (userManifest, error) {
	anchor, rel, err := stateManifestLocation(opts)
	if err != nil {
		return userManifest{}, err
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return userManifest{}, err
	}
	defer root.Close()
	data, err := readRootFile(root, rel)
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

// rollback restores every snapshotted file (rewriting prior bytes or removing a
// newly created file) and removes the directories the apply created. File and
// directory failures are collected and returned so the caller can surface that
// recovery is incomplete. A directory left non-empty by foreign or shared
// content is preserved -- rollback never force-deletes it -- but the resulting
// non-ENOENT removal error is still reported.
func (txn *userTxn) rollback() error {
	var errs []error
	for i := len(txn.files) - 1; i >= 0; i-- {
		snapshot := txn.files[i]
		if snapshot.existed {
			if err := writeAtomicInRoot(snapshot.root, snapshot.rel, snapshot.data, snapshot.mode); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", snapshot.rel, err))
			}
			continue
		}
		if err := snapshot.root.Remove(snapshot.rel); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", snapshot.rel, err))
		}
	}
	for i := len(txn.dirs) - 1; i >= 0; i-- {
		// Preserve foreign/shared content: only attempt the plain removal, and
		// report a non-ENOENT failure (e.g. a directory left non-empty).
		if err := txn.dirs[i].root.Remove(txn.dirs[i].rel); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove dir %s: %w", txn.dirs[i].rel, err))
		}
	}
	return errors.Join(errs...)
}

func rollbackUser(txn *userTxn, cause error) (UserApplyResult, error) {
	if err := txn.rollback(); err != nil {
		return UserApplyResult{}, fmt.Errorf("%w; rollback also failed: %v", cause, err)
	}
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
