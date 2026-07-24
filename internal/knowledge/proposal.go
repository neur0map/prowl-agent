package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Proposal is a reviewable candidate durable-knowledge change.
type Proposal struct {
	ID            string         `json:"id"`
	Operation     string         `json:"operation"`
	TargetPath    string         `json:"target_path"`
	CandidatePath string         `json:"candidate_path"`
	BaseHash      string         `json:"base_hash,omitempty"`
	Status        string         `json:"status"`
	Author        string         `json:"author,omitempty"`
	CreatedAt     string         `json:"created_at"`
	ReviewedAt    string         `json:"reviewed_at,omitempty"`
	Decision      *DecisionAudit `json:"decision,omitempty"`
}

// ReviewInbox manages filesystem proposals separately from accepted knowledge.
type ReviewInbox struct {
	Root       string
	Repository *Repository
	// afterCanonicalWrite is a package-private fault-injection seam used to
	// verify transactional rollback after the first canonical mutation.
	afterCanonicalWrite func() error
	// afterCanonicalWrites simulates a process interruption after all canonical
	// files are durable but before the proposal audit is committed.
	afterCanonicalWrites func()
}

func NewReviewInbox(root string, repository *Repository) *ReviewInbox {
	return &ReviewInbox{Root: root, Repository: repository}
}

// Propose validates and stores a candidate without changing accepted knowledge.
func (inbox *ReviewInbox) Propose(candidateFile, targetPath, author string, now time.Time) (*Proposal, string, error) {
	if inbox.Repository == nil || inbox.Repository.Codec == nil {
		return nil, "", fmt.Errorf("knowledge repository is required")
	}
	data, err := readRegularFile(candidateFile, MaxDocumentBytes)
	if err != nil {
		return nil, "", err
	}
	_, clean, err := inbox.Repository.resolve(targetPath)
	if err != nil {
		return nil, "", err
	}
	doc, err := inbox.Repository.Codec.Parse(clean, data)
	if err != nil {
		return nil, "", err
	}
	normalized, err := inbox.Repository.Codec.Marshal(doc)
	if err != nil {
		return nil, "", err
	}
	operation := "create"
	baseHash := ""
	old, readErr := inbox.Repository.ReadBundleFile(clean)
	if readErr == nil {
		operation = "update"
		hash := sha256.Sum256(old)
		baseHash = hex.EncodeToString(hash[:])
	} else if !os.IsNotExist(readErr) {
		return nil, "", readErr
	}
	sum := sha256.Sum256(append(append([]byte(clean+"\x00"), normalized...), []byte(now.UTC().Format(time.RFC3339Nano))...))
	id := hex.EncodeToString(sum[:])[:20]
	dir := filepath.Join(inbox.Root, id)
	if _, err := os.Stat(dir); err == nil {
		return nil, "", fmt.Errorf("proposal already exists: %s", id)
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	candidatePath := filepath.Join(dir, "candidate.md")
	proposal := &Proposal{
		ID: id, Operation: operation, TargetPath: clean,
		CandidatePath: filepath.ToSlash(filepath.Join(id, "candidate.md")),
		BaseHash:      baseHash, Status: "proposed", Author: author, CreatedAt: now.UTC().Format(time.RFC3339),
	}
	if err := atomicWrite(candidatePath, normalized, 0o644); err != nil {
		return nil, "", err
	}
	if err := inbox.writeProposal(proposal); err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", err
	}
	return proposal, documentDiff(clean, old, normalized), nil
}

// List returns proposal metadata in creation/ID order.
func (inbox *ReviewInbox) List() ([]Proposal, error) {
	entries, err := os.ReadDir(inbox.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var proposals []Proposal
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		proposal, err := inbox.load(entry.Name())
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, *proposal)
	}
	sort.Slice(proposals, func(i, j int) bool {
		if proposals[i].CreatedAt == proposals[j].CreatedAt {
			return proposals[i].ID < proposals[j].ID
		}
		return proposals[i].CreatedAt < proposals[j].CreatedAt
	})
	return proposals, nil
}

// Diff returns the deterministic review diff for a proposal.
func (inbox *ReviewInbox) Diff(id string) (string, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return "", err
	}
	candidate, err := readRootFile(inbox.Root, proposal.CandidatePath, MaxDocumentBytes)
	if err != nil {
		return "", err
	}
	old, err := inbox.Repository.ReadBundleFile(proposal.TargetPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return documentDiff(proposal.TargetPath, old, candidate), nil
}


func (inbox *ReviewInbox) accept(id string, now time.Time, decision *DecisionAudit) (*Proposal, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "proposed" {
		return nil, fmt.Errorf("proposal %s is %s", id, proposal.Status)
	}
	candidate, err := readRootFile(inbox.Root, proposal.CandidatePath, MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	doc, err := inbox.Repository.Codec.Parse(proposal.TargetPath, candidate)
	if err != nil {
		return nil, err
	}
	current, currentErr := inbox.Repository.ReadBundleFile(proposal.TargetPath)
	if proposal.Operation == "create" {
		if currentErr == nil {
			return nil, fmt.Errorf("proposal target now exists: %s", proposal.TargetPath)
		}
		if !os.IsNotExist(currentErr) {
			return nil, currentErr
		}
	} else {
		if currentErr != nil {
			return nil, fmt.Errorf("proposal target is unavailable: %s: %w", proposal.TargetPath, currentErr)
		}
		hash := sha256.Sum256(current)
		if proposal.BaseHash == "" || !strings.EqualFold(proposal.BaseHash, hex.EncodeToString(hash[:])) {
			return nil, fmt.Errorf("proposal target changed after review was created: %s", proposal.TargetPath)
		}
	}
	transaction, err := inbox.prepareAcceptTransaction(*proposal, candidate, doc, decision, now)
	if err != nil {
		return nil, err
	}
	if err := inbox.writeDecisionTransaction(transaction); err != nil {
		return nil, err
	}
	rollback := func(cause error) error {
		external, rollbackErr := restoreDecisionTransactionSnapshots(inbox.Repository, volatileSnapshots(transaction.Snapshots), volatileSnapshots(transaction.Results))
		if rollbackErr != nil {
			return errors.Join(cause, fmt.Errorf("proposal rollback failed: %w", rollbackErr))
		}
		if removeErr := inbox.removeDecisionTransaction(id); removeErr != nil {
			return errors.Join(cause, fmt.Errorf("proposal rollback cleanup failed: %w", removeErr))
		}
		if external {
			return errors.Join(cause, ErrProposalVersionConflict)
		}
		return cause
	}
	original := volatileSnapshots(transaction.Snapshots)
	results := volatileSnapshots(transaction.Results)
	if err := inbox.writeCanonicalSnapshots(results[:1], original[:1]); err != nil {
		return nil, rollback(err)
	}
	if inbox.afterCanonicalWrite != nil {
		if err := inbox.afterCanonicalWrite(); err != nil {
			return nil, rollback(err)
		}
	}
	if err := inbox.writeCanonicalSnapshots(results[1:], original[1:]); err != nil {
		return nil, rollback(err)
	}
	transaction.Stage = decisionTransactionCanonical
	if err := inbox.writeDecisionTransaction(transaction); err != nil {
		return nil, rollback(err)
	}
	if inbox.afterCanonicalWrites != nil {
		inbox.afterCanonicalWrites()
	}
	if err := inbox.writeProposal(&transaction.Proposal); err != nil {
		return nil, rollback(err)
	}
	if err := inbox.removeDecisionTransaction(id); err != nil {
		return nil, err
	}
	return &transaction.Proposal, nil
}


func (inbox *ReviewInbox) reject(id string, now time.Time, decision *DecisionAudit) (*Proposal, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "proposed" {
		return nil, fmt.Errorf("proposal %s is %s", id, proposal.Status)
	}
	proposal.Status = "rejected"
	proposal.ReviewedAt = now.UTC().Format(time.RFC3339)
	proposal.Decision = decision
	if err := inbox.writeProposal(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (inbox *ReviewInbox) load(id string) (*Proposal, error) {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return nil, fmt.Errorf("invalid proposal id %q", id)
	}
	data, err := readRootFile(inbox.Root, filepath.ToSlash(filepath.Join(id, "proposal.json")), MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	var proposal Proposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		return nil, err
	}
	if proposal.ID != id {
		return nil, fmt.Errorf("proposal id mismatch: requested %s, found %s", id, proposal.ID)
	}
	return &proposal, nil
}

func (inbox *ReviewInbox) writeProposal(proposal *Proposal) error {
	data, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(inbox.Root, proposal.ID, "proposal.json"), data, 0o644)
}

func documentDiff(path string, old, candidate []byte) string {
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n@@ -1 +1 @@\n", filepath.ToSlash(path), filepath.ToSlash(path))
	for _, line := range splitDiffLines(old) {
		out.WriteString("-" + line + "\n")
	}
	for _, line := range splitDiffLines(candidate) {
		out.WriteString("+" + line + "\n")
	}
	return out.String()
}

func splitDiffLines(data []byte) []string {
	text := strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

type fileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func snapshotBundleFiles(repository *Repository, paths ...string) ([]fileSnapshot, error) {
	root, err := os.OpenRoot(repository.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	out := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		data, err := repository.ReadBundleFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		entry := fileSnapshot{path: filepath.ToSlash(path), data: data, exists: err == nil, mode: 0o644}
		if info, statErr := root.Lstat(filepath.FromSlash(path)); statErr == nil {
			entry.mode = info.Mode().Perm()
		}
		out = append(out, entry)
	}
	return out, nil
}

func restoreBundleSnapshots(repository *Repository, snapshots []fileSnapshot) error {
	root, err := os.OpenRoot(repository.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	var restoreErrors []error
	for _, snapshot := range snapshots {
		if snapshot.exists {
			if err := atomicWriteInRoot(root, snapshot.path, snapshot.data, snapshot.mode); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("restore %s: %w", snapshot.path, err))
			}
		} else {
			if err := root.Remove(filepath.FromSlash(snapshot.path)); err != nil && !os.IsNotExist(err) {
				restoreErrors = append(restoreErrors, fmt.Errorf("remove %s: %w", snapshot.path, err))
			}
		}
	}
	return errors.Join(restoreErrors...)
}
