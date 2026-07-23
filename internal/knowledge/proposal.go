package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Proposal is a reviewable candidate durable-knowledge change.
type Proposal struct {
	ID            string `json:"id"`
	Operation     string `json:"operation"`
	TargetPath    string `json:"target_path"`
	CandidatePath string `json:"candidate_path"`
	Status        string `json:"status"`
	Author        string `json:"author,omitempty"`
	CreatedAt     string `json:"created_at"`
	ReviewedAt    string `json:"reviewed_at,omitempty"`
}

// ReviewInbox manages filesystem proposals separately from accepted knowledge.
type ReviewInbox struct {
	Root       string
	Repository *Repository
}

func NewReviewInbox(root string, repository *Repository) *ReviewInbox {
	return &ReviewInbox{Root: root, Repository: repository}
}

// Propose validates and stores a candidate without changing accepted knowledge.
func (inbox *ReviewInbox) Propose(candidateFile, targetPath, author string, now time.Time) (*Proposal, string, error) {
	if inbox.Repository == nil || inbox.Repository.Codec == nil {
		return nil, "", fmt.Errorf("knowledge repository is required")
	}
	data, err := os.ReadFile(candidateFile)
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
	target, _, _ := inbox.Repository.resolve(clean)
	operation := "create"
	old, readErr := os.ReadFile(target)
	if readErr == nil {
		operation = "update"
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
		Status:        "proposed", Author: author, CreatedAt: now.UTC().Format(time.RFC3339),
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
	candidate, err := os.ReadFile(filepath.Join(inbox.Root, filepath.FromSlash(proposal.CandidatePath)))
	if err != nil {
		return "", err
	}
	target, _, err := inbox.Repository.resolve(proposal.TargetPath)
	if err != nil {
		return "", err
	}
	old, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return documentDiff(proposal.TargetPath, old, candidate), nil
}

// Accept applies a proposal and rolls canonical files back if any later step fails.
func (inbox *ReviewInbox) Accept(id string, now time.Time) (*Proposal, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "proposed" {
		return nil, fmt.Errorf("proposal %s is %s", id, proposal.Status)
	}
	candidatePath := filepath.Join(inbox.Root, filepath.FromSlash(proposal.CandidatePath))
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return nil, err
	}
	doc, err := inbox.Repository.Codec.Parse(proposal.TargetPath, candidate)
	if err != nil {
		return nil, err
	}
	target, _, err := inbox.Repository.resolve(proposal.TargetPath)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(target)
	if proposal.Operation == "create" && statErr == nil {
		return nil, fmt.Errorf("proposal target now exists: %s", proposal.TargetPath)
	}
	if proposal.Operation == "update" && os.IsNotExist(statErr) {
		return nil, fmt.Errorf("proposal target no longer exists: %s", proposal.TargetPath)
	}
	backups, err := snapshotFiles(target, filepath.Join(inbox.Repository.Root, "index.md"), filepath.Join(inbox.Repository.Root, "log.md"))
	if err != nil {
		return nil, err
	}
	rollback := func() { restoreSnapshots(backups) }
	if err := inbox.Repository.Write(doc); err != nil {
		return nil, err
	}
	if err := inbox.Repository.AppendLog("accepted", proposal.TargetPath, now); err != nil {
		rollback()
		return nil, err
	}
	if err := inbox.Repository.GenerateIndex(); err != nil {
		rollback()
		return nil, err
	}
	proposal.Status = "accepted"
	proposal.ReviewedAt = now.UTC().Format(time.RFC3339)
	if err := inbox.writeProposal(proposal); err != nil {
		rollback()
		return nil, err
	}
	return proposal, nil
}

// Reject records review without touching canonical knowledge.
func (inbox *ReviewInbox) Reject(id string, now time.Time) (*Proposal, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "proposed" {
		return nil, fmt.Errorf("proposal %s is %s", id, proposal.Status)
	}
	proposal.Status = "rejected"
	proposal.ReviewedAt = now.UTC().Format(time.RFC3339)
	if err := inbox.writeProposal(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func (inbox *ReviewInbox) load(id string) (*Proposal, error) {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return nil, fmt.Errorf("invalid proposal id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(inbox.Root, id, "proposal.json"))
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

func snapshotFiles(paths ...string) ([]fileSnapshot, error) {
	out := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		entry := fileSnapshot{path: path, data: data, exists: err == nil, mode: 0o644}
		if info, statErr := os.Stat(path); statErr == nil {
			entry.mode = info.Mode().Perm()
		}
		out = append(out, entry)
	}
	return out, nil
}

func restoreSnapshots(snapshots []fileSnapshot) {
	for _, snapshot := range snapshots {
		if snapshot.exists {
			_ = atomicWrite(snapshot.path, snapshot.data, snapshot.mode)
		} else {
			_ = os.Remove(snapshot.path)
		}
	}
}
