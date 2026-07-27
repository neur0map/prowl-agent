package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/operations"
	"github.com/prowl-agent/prowl-agent/internal/session"
)

func newSessionCmd() *cobra.Command {
	command := &cobra.Command{Use: "session", Short: "Create, resume, and inspect durable agent sessions"}
	command.AddCommand(newSessionStartCmd(nil), newSessionTurnCmd(nil), newSessionShowCmd(nil), newSessionResumeCmd(nil))
	return command
}

// resolveSessionService returns the injected service (tests) or, when nil, a
// service bound to the single operations database on the trusted CLI surface.
func resolveSessionService(ctx context.Context, injected session.Service) (session.Service, func(), error) {
	if injected != nil {
		return injected, func() {}, nil
	}
	store, err := operations.Open(ctx)
	if err != nil {
		return nil, nil, err
	}
	return session.NewService(store, operations.SurfaceCLI), func() { _ = store.Close() }, nil
}

func newSessionStartCmd(svc session.Service) *cobra.Command {
	var snapshotPath, exposurePath, parent string
	var asJSON bool
	command := &cobra.Command{
		Use:   "start",
		Short: "Start a session pinning an immutable prompt snapshot and exposure manifest",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			snapshotBytes, err := os.ReadFile(snapshotPath)
			if err != nil {
				return err
			}
			exposureBytes, err := os.ReadFile(exposurePath)
			if err != nil {
				return err
			}
			service, closeService, err := resolveSessionService(command.Context(), svc)
			if err != nil {
				return err
			}
			defer closeService()
			view, err := service.CreateSession(command.Context(), session.CreateSessionRequest{
				SnapshotBytes:   snapshotBytes,
				ExposureBytes:   exposureBytes,
				ParentSessionID: parent,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeSessionJSON(command, view)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Session %s (version %d, %s)\nSnapshot %s\nExposure %s\n", view.ID, view.Version, view.Status, view.SnapshotID, view.ExposureID)
			return err
		},
	}
	command.Flags().StringVar(&snapshotPath, "snapshot", "", "path to the pinned B0.2 snapshot canonical JSON")
	command.Flags().StringVar(&exposurePath, "exposure", "", "path to the pinned B0.2 exposure manifest canonical JSON")
	command.Flags().StringVar(&parent, "parent", "", "optional parent session id")
	command.Flags().BoolVar(&asJSON, "json", false, "emit the session as JSON")
	_ = command.MarkFlagRequired("snapshot")
	_ = command.MarkFlagRequired("exposure")
	return command
}

func newSessionTurnCmd(svc session.Service) *cobra.Command {
	var sessionID, key, run, status string
	var expected int64
	var messages []string
	var asJSON bool
	command := &cobra.Command{
		Use:   "turn",
		Short: "Append a turn trajectory to a session",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, closeService, err := resolveSessionService(command.Context(), svc)
			if err != nil {
				return err
			}
			defer closeService()
			entries := make([]session.TurnEntryInput, 0, len(messages))
			for _, message := range messages {
				entries = append(entries, session.TurnEntryInput{Kind: session.EntryMessage, Body: message, Metadata: session.EntryMetadata{Role: "user"}})
			}
			view, err := service.AppendTurn(command.Context(), session.AppendTurnRequest{
				SessionID:       sessionID,
				IdempotencyKey:  key,
				ExpectedVersion: expected,
				RunID:           run,
				Status:          session.TurnStatus(status),
				Entries:         entries,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeSessionJSON(command, view)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Turn %s ordinal %d -> version %d (%s)\n", view.ID, view.Ordinal, view.ResultingVersion, view.Status)
			return err
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "session id")
	command.Flags().StringVar(&key, "idempotency-key", "", "idempotency key preventing duplicate turns")
	command.Flags().StringVar(&run, "run", "", "run id")
	command.Flags().StringVar(&status, "status", string(session.TurnSucceeded), "turn status: queued, running, succeeded, failed, or cancelled")
	command.Flags().Int64Var(&expected, "expected-version", 0, "expected session version for optimistic concurrency")
	command.Flags().StringArrayVar(&messages, "message", nil, "append a user message entry (repeatable)")
	command.Flags().BoolVar(&asJSON, "json", false, "emit the turn as JSON")
	_ = command.MarkFlagRequired("session")
	_ = command.MarkFlagRequired("idempotency-key")
	_ = command.MarkFlagRequired("run")
	_ = command.MarkFlagRequired("expected-version")
	return command
}

func newSessionShowCmd(svc session.Service) *cobra.Command {
	var sessionID string
	var asJSON bool
	command := &cobra.Command{
		Use:   "show",
		Short: "Show a session ledger and its turns",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, closeService, err := resolveSessionService(command.Context(), svc)
			if err != nil {
				return err
			}
			defer closeService()
			view, err := service.GetSession(command.Context(), session.GetSessionRequest{SessionID: sessionID})
			if err != nil {
				return err
			}
			if asJSON {
				return writeSessionJSON(command, view)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Session %s (version %d, %s)\nSurface %s owner %s\nTurns %d\n", view.ID, view.Version, view.Status, view.SurfaceID, view.OwnerPrincipalID, len(view.Turns))
			return err
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "session id")
	command.Flags().BoolVar(&asJSON, "json", false, "emit the session as JSON")
	_ = command.MarkFlagRequired("session")
	return command
}

func newSessionResumeCmd(svc session.Service) *cobra.Command {
	var sessionID string
	command := &cobra.Command{
		Use:   "resume",
		Short: "Resume a session by emitting its pinned immutable snapshot",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, closeService, err := resolveSessionService(command.Context(), svc)
			if err != nil {
				return err
			}
			defer closeService()
			view, err := service.GetSession(command.Context(), session.GetSessionRequest{SessionID: sessionID})
			if err != nil {
				return err
			}
			// Resume returns the exact pinned snapshot bytes, never re-resolving
			// current mutable state.
			_, err = command.OutOrStdout().Write(view.SnapshotBytes)
			return err
		},
	}
	command.Flags().StringVar(&sessionID, "session", "", "session id")
	_ = command.MarkFlagRequired("session")
	return command
}

func writeSessionJSON(command *cobra.Command, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), string(encoded))
	return err
}
