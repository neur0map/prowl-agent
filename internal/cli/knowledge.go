package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

type knowledgeSummary struct {
	Path        string   `json:"path"`
	ID          string   `json:"id,omitempty"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func newKnowledgeCmd() *cobra.Command {
	command := &cobra.Command{Use: "knowledge", Short: "Manage portable, reviewable project knowledge"}
	command.AddCommand(
		newKnowledgeInitCmd(), newKnowledgeListCmd(), newKnowledgeShowCmd(), newKnowledgeLintCmd(),
		newKnowledgeProposeCmd(), newKnowledgeAcceptCmd(), newKnowledgeRejectCmd(), newKnowledgeExportCmd(),
	)
	return command
}

func knowledgeWorkspace() (*workspace.Workspace, *knowledge.Repository, *knowledge.ReviewInbox, error) {
	ws, err := workspace.Resolve(".")
	if err != nil {
		return nil, nil, nil, err
	}
	repo := knowledge.NewRepository(ws.Knowledge, okfv01.Codec{})
	return ws, repo, knowledge.NewReviewInbox(ws.Proposals, repo), nil
}

func syncKnowledge(ws *workspace.Workspace, repo *knowledge.Repository) error {
	db, err := store.Open(ws.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	return repo.SyncStore(db, ws.Root, time.Now().UTC())
}

func newKnowledgeInitCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "init", Short: "Initialize the trackable OKF knowledge bundle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, repo, _, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			if err := repo.Init(); err != nil {
				return err
			}
			if err := workspace.EnsureDerivedIgnored(ws.Root); err != nil {
				return err
			}
			if err := syncKnowledge(ws, repo); err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"knowledge_root": ws.Knowledge, "initialized": true})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Knowledge bundle ready: %s\n", ws.Knowledge)
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeListCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "list", Short: "List accepted knowledge documents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, repo, _, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			docs, err := repo.List()
			if err != nil {
				return err
			}
			summaries := summarizeKnowledge(docs)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(summaries)
			}
			if len(summaries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No accepted knowledge yet. Add a proposal with 'prowl-agent knowledge propose'.")
				return nil
			}
			for _, summary := range summaries {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-10s %s — %s\n", summary.Type, summary.Status, summary.Path, summary.Title)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeShowCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "show <id-or-path>", Args: cobra.ExactArgs(1), Short: "Show one accepted knowledge document",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, repo, _, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			docs, err := repo.List()
			if err != nil {
				return err
			}
			for _, doc := range docs {
				if doc.Path != args[0] && doc.Prowl.ID != args[0] {
					continue
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"summary": summarizeKnowledge([]*knowledge.Document{doc})[0], "body": string(doc.Body), "resource": doc.Resource, "anchors": doc.Prowl.Anchors})
				}
				data, err := repo.Codec.Marshal(doc)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			return fmt.Errorf("knowledge document not found: %s", args[0])
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeLintCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "lint", Short: "Check links, IDs, temporal ranges, evidence, and anchor freshness",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, repo, _, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			findings, err := repo.Lint(ws.Root)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(findings)
			}
			if len(findings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Knowledge health: no findings.")
				return nil
			}
			for _, finding := range findings {
				fmt.Fprintf(cmd.OutOrStdout(), "[%-7s] %-36s %s\n            %s\n", strings.ToUpper(finding.Severity), finding.Code, finding.Path, finding.Message)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeProposeCmd() *cobra.Command {
	var file, target, author string
	var asJSON bool
	command := &cobra.Command{
		Use: "propose", Short: "Add a validated candidate to the review inbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			if target == "" {
				target = filepath.Base(file)
			}
			_, _, inbox, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			proposal, diff, err := inbox.Propose(file, target, author, time.Now().UTC())
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"proposal": proposal, "diff": diff})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Proposal %s (%s)\n%s", proposal.ID, proposal.Operation, diff)
			return nil
		},
	}
	command.Flags().StringVar(&file, "file", "", "candidate OKF Markdown file")
	command.Flags().StringVar(&target, "target", "", "bundle-relative destination path")
	command.Flags().StringVar(&author, "author", "", "proposal author or source")
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeAcceptCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "accept <proposal-id>", Args: cobra.ExactArgs(1), Short: "Accept a reviewed proposal atomically",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, repo, inbox, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			diff, err := inbox.Diff(args[0])
			if err != nil {
				return err
			}
			proposal, err := inbox.Accept(args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			if err := syncKnowledge(ws, repo); err != nil {
				return fmt.Errorf("proposal accepted but derived metadata refresh failed: %w", err)
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"proposal": proposal, "diff": diff})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%sAccepted proposal %s into %s.\n", diff, proposal.ID, proposal.TargetPath)
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeRejectCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "reject <proposal-id>", Args: cobra.ExactArgs(1), Short: "Reject a proposal without changing accepted knowledge",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, inbox, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			proposal, err := inbox.Reject(args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(proposal)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rejected proposal %s.\n", proposal.ID)
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func newKnowledgeExportCmd() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "export <directory>", Args: cobra.ExactArgs(1), Short: "Export the complete portable OKF bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, repo, _, err := knowledgeWorkspace()
			if err != nil {
				return err
			}
			destination, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if err := repo.Export(destination); err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"exported_to": destination})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exported knowledge bundle to %s\n", destination)
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return command
}

func summarizeKnowledge(docs []*knowledge.Document) []knowledgeSummary {
	out := make([]knowledgeSummary, 0, len(docs))
	for _, doc := range docs {
		out = append(out, knowledgeSummary{Path: doc.Path, ID: doc.Prowl.ID, Type: doc.Type, Title: doc.Title, Description: doc.Description, Status: doc.Prowl.Status, Tags: doc.Tags})
	}
	return out
}
