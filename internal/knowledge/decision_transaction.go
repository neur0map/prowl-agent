package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	decisionTransactionFile                = "decision-transaction.json"
	decisionTransactionSchemaVersion       = "prowl.knowledge-decision-transaction/v1"
	decisionTransactionPrepared            = "prepared"
	decisionTransactionCanonical           = "canonical"
	maxDecisionTransactionBytes      int64 = 48 << 20
)

type decisionFileSnapshot struct {
	Path   string      `json:"path"`
	Data   []byte      `json:"data"`
	Mode   os.FileMode `json:"mode"`
	Exists bool        `json:"exists"`
}

type decisionTransaction struct {
	SchemaVersion string                 `json:"schema_version"`
	Stage         string                 `json:"stage"`
	Proposal      Proposal               `json:"proposal"`
	Audit         DecisionAudit          `json:"audit"`
	Candidate     []byte                 `json:"candidate"`
	Snapshots     []decisionFileSnapshot `json:"snapshots"`
	Results       []decisionFileSnapshot `json:"results"`
}

func (inbox *ReviewInbox) prepareAcceptTransaction(proposal Proposal, candidate []byte, doc *Document, audit *DecisionAudit, now time.Time) (*decisionTransaction, error) {
	if audit == nil {
		return nil, errors.New("proposal decision audit is required")
	}
	snapshots, err := snapshotBundleFiles(inbox.Repository, proposal.TargetPath, "index.md", "log.md")
	if err != nil {
		return nil, err
	}
	results, err := inbox.acceptResults(doc, snapshots, now)
	if err != nil {
		return nil, err
	}
	final := proposal
	final.Status = "accepted"
	final.ReviewedAt = now.UTC().Format(time.RFC3339)
	final.Decision = audit
	return &decisionTransaction{
		SchemaVersion: decisionTransactionSchemaVersion,
		Stage:         decisionTransactionPrepared,
		Proposal:      final,
		Audit:         *audit,
		Candidate:     append([]byte(nil), candidate...),
		Snapshots:     persistentSnapshots(snapshots),
		Results:       persistentSnapshots(results),
	}, nil
}

func (inbox *ReviewInbox) acceptResults(doc *Document, snapshots []fileSnapshot, now time.Time) ([]fileSnapshot, error) {
	if len(snapshots) != 3 || snapshots[0].path != doc.Path || snapshots[1].path != "index.md" || snapshots[2].path != "log.md" {
		return nil, errors.New("invalid canonical rollback snapshots")
	}
	target, err := inbox.Repository.Codec.Marshal(doc)
	if err != nil {
		return nil, err
	}
	log := snapshots[2].data
	if !snapshots[2].exists {
		log = []byte("# Knowledge log\n")
	}
	if len(log) > 0 && !bytes.HasSuffix(log, []byte("\n")) {
		log = append(log, '\n')
	}
	log = append(log, fmt.Sprintf("- %s \u2014 accepted `%s`\n", now.UTC().Format(time.RFC3339), filepath.ToSlash(doc.Path))...)

	documents, err := inbox.Repository.List()
	if err != nil {
		return nil, err
	}
	replaced := false
	for i, current := range documents {
		if current.Path == doc.Path {
			documents[i] = doc
			replaced = true
			break
		}
	}
	if !replaced {
		documents = append(documents, doc)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	index := snapshots[1].data
	if !snapshots[1].exists {
		index = []byte("# Knowledge\n")
	}
	var generated strings.Builder
	generated.WriteString(indexStart + "\n")
	for _, current := range documents {
		title := current.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(current.Path), filepath.Ext(current.Path))
		}
		generated.WriteString(fmt.Sprintf("- [%s](%s) \u2014 %s\n", title, current.Path, current.Type))
	}
	if len(documents) == 0 {
		generated.WriteString("_No concepts yet._\n")
	}
	generated.WriteString(indexEnd + "\n")
	index = replaceOwnedBlock(index, []byte(generated.String()))

	return []fileSnapshot{
		{path: doc.Path, data: target, mode: snapshots[0].mode, exists: true},
		{path: "index.md", data: index, mode: 0o644, exists: true},
		{path: "log.md", data: log, mode: 0o644, exists: true},
	}, nil
}

func (inbox *ReviewInbox) recoverDecisionTransaction(id string) error {
	transaction, err := inbox.readDecisionTransaction(id)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := inbox.validDecisionTransaction(transaction, id); err != nil {
		return err
	}
	original := volatileSnapshots(transaction.Snapshots)
	results := volatileSnapshots(transaction.Results)
	current, err := snapshotBundleFiles(inbox.Repository, transaction.Proposal.TargetPath, "index.md", "log.md")
	if err != nil {
		return err
	}
	candidate, candidateErr := readRootFile(inbox.Root, transaction.Proposal.CandidatePath, MaxDocumentBytes)
	candidateMatches := candidateErr == nil && bytes.Equal(candidate, transaction.Candidate)
	if candidateErr != nil && !os.IsNotExist(candidateErr) {
		return candidateErr
	}
	if candidateMatches && snapshotsMatch(current, results) {
		stored, err := inbox.load(id)
		if err != nil {
			return err
		}
		if stored.Decision != nil {
			if !sameProposal(*stored, transaction.Proposal) {
				return errors.New("proposal decision transaction conflicts with proposal record")
			}
			return inbox.removeDecisionTransaction(id)
		}
		if stored.Status != "proposed" || !sameProposal(*stored, transaction.Proposal, "Decision", "Status", "ReviewedAt") {
			return errors.New("proposal decision transaction conflicts with proposal record")
		}
		if err := inbox.writeProposal(&transaction.Proposal); err != nil {
			return err
		}
		return inbox.removeDecisionTransaction(id)
	}
	external, err := restoreDecisionTransactionSnapshots(inbox.Repository, original, results)
	if err != nil {
		return fmt.Errorf("proposal decision transaction rollback failed: %w", err)
	}
	if err := inbox.removeDecisionTransaction(id); err != nil {
		return err
	}
	if !candidateMatches || external {
		return ErrProposalVersionConflict
	}
	return nil
}

func restoreDecisionTransactionSnapshots(repository *Repository, original, results []fileSnapshot) (bool, error) {
	if len(original) != len(results) || len(original) == 0 {
		return false, errors.New("invalid decision transaction snapshots")
	}
	paths := make([]string, len(original))
	for index := range original {
		if original[index].path != results[index].path {
			return false, errors.New("invalid decision transaction snapshot paths")
		}
		paths[index] = original[index].path
	}
	current, err := snapshotBundleFiles(repository, paths...)
	if err != nil {
		return false, err
	}
	external := false
	for index := range current {
		switch {
		case sameSnapshot(current[index], original[index]):
		case sameSnapshot(current[index], results[index]):
			if err := restoreBundleSnapshots(repository, original[index:index+1]); err != nil {
				return false, err
			}
		default:
			external = true
		}
	}
	after, err := snapshotBundleFiles(repository, paths...)
	if err != nil {
		return false, err
	}
	for index := range after {
		if !sameSnapshot(original[index], results[index]) && sameSnapshot(after[index], results[index]) {
			return false, fmt.Errorf("transaction result remains at %s", after[index].path)
		}
		if !sameSnapshot(after[index], original[index]) {
			external = true
		}
	}
	return external, nil
}

func (inbox *ReviewInbox) writeCanonicalSnapshots(snapshots, expected []fileSnapshot) error {
	if len(snapshots) != len(expected) {
		return errors.New("canonical snapshot count mismatch")
	}
	root, err := os.OpenRoot(inbox.Repository.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for index, snapshot := range snapshots {
		if !snapshot.exists || snapshot.path != expected[index].path {
			return errors.New("invalid canonical result")
		}
		current, err := snapshotBundleFiles(inbox.Repository, snapshot.path)
		if err != nil {
			return err
		}
		if !sameSnapshot(current[0], expected[index]) {
			return ErrProposalVersionConflict
		}
		if err := atomicWriteInRoot(root, snapshot.path, snapshot.data, snapshot.mode); err != nil {
			return err
		}
	}
	return nil
}

func (inbox *ReviewInbox) readDecisionTransaction(id string) (*decisionTransaction, error) {
	data, err := readRootFile(inbox.Root, filepath.ToSlash(filepath.Join(id, decisionTransactionFile)), maxDecisionTransactionBytes)
	if err != nil {
		return nil, err
	}
	var transaction decisionTransaction
	if err := json.Unmarshal(data, &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (inbox *ReviewInbox) writeDecisionTransaction(transaction *decisionTransaction) error {
	if err := inbox.validDecisionTransaction(transaction, transaction.Proposal.ID); err != nil {
		return err
	}
	data, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	if int64(len(data)) > maxDecisionTransactionBytes {
		return errors.New("proposal decision transaction exceeds bounds")
	}
	root, err := os.OpenRoot(inbox.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicWriteInRoot(root, filepath.ToSlash(filepath.Join(transaction.Proposal.ID, decisionTransactionFile)), data, 0o600)
}

func (inbox *ReviewInbox) removeDecisionTransaction(id string) error {
	root, err := os.OpenRoot(inbox.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(filepath.FromSlash(filepath.Join(id, decisionTransactionFile))); err != nil && !os.IsNotExist(err) {
		return err
	}
	directory, err := root.Open(filepath.FromSlash(id))
	if err != nil {
		return err
	}
	defer directory.Close()
	return syncDirectoryAfterRename(directory)
}

func (inbox *ReviewInbox) validDecisionTransaction(transaction *decisionTransaction, id string) error {
	if transaction == nil || transaction.SchemaVersion != decisionTransactionSchemaVersion || transaction.Stage != decisionTransactionPrepared && transaction.Stage != decisionTransactionCanonical || transaction.Proposal.ID != id || transaction.Proposal.Status != "accepted" || transaction.Proposal.Decision == nil || !sameAudit(*transaction.Proposal.Decision, transaction.Audit) || int64(len(transaction.Candidate)) > MaxDocumentBytes || len(transaction.Snapshots) != 3 || len(transaction.Results) != 3 {
		return errors.New("invalid proposal decision transaction")
	}
	if err := validStoredDecisionAudit(transaction.Audit, transaction.Proposal, transaction.Audit.VersionAfter); err != nil {
		return err
	}
	paths := transaction.Audit.Rollback.Paths
	for index, snapshot := range transaction.Snapshots {
		if snapshot.Path != paths[index] || transaction.Results[index].Path != paths[index] || int64(len(snapshot.Data)) > MaxDocumentBytes || int64(len(transaction.Results[index].Data)) > MaxDocumentBytes || snapshot.Mode.Perm() != snapshot.Mode || transaction.Results[index].Mode.Perm() != transaction.Results[index].Mode {
			return errors.New("invalid proposal decision transaction snapshot")
		}
	}
	return nil
}

func persistentSnapshots(snapshots []fileSnapshot) []decisionFileSnapshot {
	out := make([]decisionFileSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		out[i] = decisionFileSnapshot{Path: snapshot.path, Data: append([]byte(nil), snapshot.data...), Mode: snapshot.mode, Exists: snapshot.exists}
	}
	return out
}

func volatileSnapshots(snapshots []decisionFileSnapshot) []fileSnapshot {
	out := make([]fileSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		out[i] = fileSnapshot{path: snapshot.Path, data: snapshot.Data, mode: snapshot.Mode, exists: snapshot.Exists}
	}
	return out
}

func snapshotsMatch(left, right []fileSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !sameSnapshot(left[i], right[i]) {
			return false
		}
	}
	return true
}

func sameSnapshot(left, right fileSnapshot) bool {
	return left.path == right.path && left.exists == right.exists && left.mode.Perm() == right.mode.Perm() && bytes.Equal(left.data, right.data)
}

func sameAudit(left, right DecisionAudit) bool {
	return left.SchemaVersion == right.SchemaVersion && left.ProposalID == right.ProposalID && left.Action == right.Action && left.IdempotencyKey == right.IdempotencyKey && left.PrincipalID == right.PrincipalID && left.ExpectedVersion == right.ExpectedVersion && left.VersionBefore == right.VersionBefore && left.VersionAfter == right.VersionAfter && left.DecidedAt == right.DecidedAt && strings.Join(left.Rollback.Paths, "\x00") == strings.Join(right.Rollback.Paths, "\x00")
}

func sameProposal(left, right Proposal, ignored ...string) bool {
	ignore := make(map[string]bool, len(ignored))
	for _, field := range ignored {
		ignore[field] = true
	}
	if !ignore["ID"] && left.ID != right.ID || !ignore["Operation"] && left.Operation != right.Operation || !ignore["TargetPath"] && left.TargetPath != right.TargetPath || !ignore["CandidatePath"] && left.CandidatePath != right.CandidatePath || !ignore["BaseHash"] && left.BaseHash != right.BaseHash || !ignore["Status"] && left.Status != right.Status || !ignore["Author"] && left.Author != right.Author || !ignore["CreatedAt"] && left.CreatedAt != right.CreatedAt || !ignore["ReviewedAt"] && left.ReviewedAt != right.ReviewedAt {
		return false
	}
	if !ignore["Decision"] {
		return left.Decision != nil && right.Decision != nil && sameAudit(*left.Decision, *right.Decision)
	}
	return true
}
