// Package application owns deterministic project service assembly shared by
// CLI, MCP, and workbench transports.
package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/flock"
	"github.com/prowl-agent/prowl-agent/internal/assist"
	"github.com/prowl-agent/prowl-agent/internal/capability"
	"github.com/prowl-agent/prowl-agent/internal/config"
	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/knowledge/okfv01"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// InferencerProvider resolves an optional project inferencer from configuration.
type InferencerProvider func(context.Context, config.Config) assist.Inferencer

// Options controls optional AI assembly. Deterministic structural services are
// always available and never require an inferencer.
type Options struct {
	EnableAI           bool
	InferencerProvider InferencerProvider
}

// inferencerEmbeds reports whether an inferencer can produce embeddings. A
// provider may opt out (e.g. an agent-CLI backend that only reranks) so the
// refresh skips vector building instead of recording a spurious embed error.
func inferencerEmbeds(inf assist.Inferencer) bool {
	if c, ok := inf.(interface{ SupportsEmbeddings() bool }); ok {
		return c.SupportsEmbeddings()
	}
	return true
}

// embedModelID returns the embedding model identity an inferencer actually uses
// for vectors (the in-process static model, or an Ollama model name), falling
// back to the configured name when the backend reports none. It keys stored
// vectors so switching backends triggers a clean re-embed.
func embedModelID(inf assist.Inferencer, fallback string) string {
	if m, ok := inf.(interface{ EmbedModelID() string }); ok {
		if id := m.EmbedModelID(); id != "" {
			return id
		}
	}
	return fallback
}

// RefreshResult describes one deterministic refresh. EmbeddingError is a
// best-effort AI warning; structural indexing failures are returned as errors.
type RefreshResult struct {
	Summary        index.Summary
	Embedded       int
	EmbeddingError error
}

// Project is the shared, deterministic project service graph. It starts no
// goroutines. Close owns the underlying Store and is idempotent.
type Project struct {
	Workspace    *workspace.Workspace
	Config       config.Config
	Store        *store.Store
	Capabilities *capability.Catalog
	Query        *query.Querier
	Context      *contextpacket.Service
	Knowledge    *knowledge.Repository
	Inferencer   assist.Inferencer
	ReadGuard    store.ReadGuard
	// InitialRefresh is non-zero when OpenProject repaired or refreshed stale
	// project state during assembly.
	InitialRefresh RefreshResult

	refreshGate chan struct{}
	closeOnce   sync.Once
	closeErr    error
	closed      atomic.Bool
	// beforeIndex and afterIndex are package-private deterministic test seams.
	beforeIndex func()
	// afterIndex is a package-private test seam invoked while generation writes
	// are still uncommitted and before post-index signature validation.
	afterIndex func()
}

// OpenProject resolves a workspace, opens its derived store, loads strict
// configuration, refreshes stale structural data, and assembles shared services.
func OpenProject(ctx context.Context, start string, opts Options) (*Project, error) {
	project, err := assembleProject(ctx, start, opts, func(_ context.Context, start string) (*workspace.Workspace, error) {
		return workspace.Resolve(start)
	}, func(_ context.Context, path string) (config.Config, error) {
		return config.Load(path)
	}, func(_ context.Context, path string) (*store.Store, error) {
		return store.Open(path)
	})
	if err != nil {
		return nil, err
	}
	initialRefresh, err := project.ensureFresh(ctx)
	if err != nil {
		return nil, errors.Join(err, project.Close())
	}
	project.InitialRefresh = initialRefresh
	return project, nil
}

func assembleProject(
	ctx context.Context,
	start string,
	opts Options,
	resolveWorkspace func(context.Context, string) (*workspace.Workspace, error),
	loadConfig func(context.Context, string) (config.Config, error),
	openStore func(context.Context, string) (*store.Store, error),
) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := resolveWorkspace(ctx, start)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	database, err := openStore(ctx, state.DB)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Project, error) {
		return nil, errors.Join(err, database.Close())
	}
	cfg, err := loadConfig(ctx, state.Path)
	if err != nil {
		return fail(fmt.Errorf("load project config: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	var inferencer assist.Inferencer
	if opts.EnableAI && opts.InferencerProvider != nil {
		inferencer = opts.InferencerProvider(ctx, cfg)
	}

	catalog, err := capability.BuiltinCatalog()
	if err != nil {
		return fail(fmt.Errorf("load capability catalog: %w", err))
	}
	knowledgeRepo := knowledge.NewRepository(state.Knowledge, okfv01.Codec{})
	querier := query.New(database)
	if inferencer != nil {
		querier = query.NewWithAssist(database, inferencer)
	}
	readGuard := generationReadGuard(filepath.Join(state.Path, "index-refresh.lock"))
	querier.RequirePublishedGeneration().WithReadGuard(readGuard)
	contextService := &contextpacket.Service{Store: database, Knowledge: knowledgeRepo, Root: state.Root, Tracer: contextpacket.StoreTracer{Store: database}, RequirePublished: true, ReadGuard: readGuard}
	if inferencer != nil {
		reranker := contextpacket.AssistSemanticReranker{Inferencer: inferencer}
		// Agent CLIs answer via a subprocess completion, slower than a local
		// daemon, so give reranking a longer budget before it falls back to the
		// deterministic order.
		if _, ok := inferencer.(*assist.AgentCLI); ok {
			reranker.Timeout = 45 * time.Second
		}
		contextService.Reranker = reranker
	}
	project := &Project{
		Workspace:    state,
		Config:       cfg,
		Store:        database,
		Capabilities: catalog,
		Query:        querier,
		Context:      contextService,
		Knowledge:    knowledgeRepo,
		Inferencer:   inferencer,
		ReadGuard:    readGuard,
		refreshGate:  make(chan struct{}, 1),
	}
	if err := database.SetMetaContext(ctx, "ai_enabled", strconv.FormatBool(cfg.AI.Enabled)); err != nil {
		return fail(fmt.Errorf("record AI state: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return project, nil
}

// Refresh performs the shared deterministic reindex operation used by startup
// and transport-owned freshness watchers. It serializes refreshes but starts no
// background work itself.
func (p *Project) Refresh(ctx context.Context) (RefreshResult, error) {
	return p.refreshWithProgress(ctx, nil)
}

func (p *Project) refreshWithProgress(ctx context.Context, report index.ProgressReporter) (RefreshResult, error) {
	if p.closed.Load() {
		return RefreshResult{}, errors.New("project is closed")
	}
	select {
	case p.refreshGate <- struct{}{}:
		defer func() { <-p.refreshGate }()
	case <-ctx.Done():
		return RefreshResult{}, ctx.Err()
	}
	if p.closed.Load() {
		return RefreshResult{}, errors.New("project is closed")
	}

	fileLock := flock.New(filepath.Join(p.Workspace.Path, "index-refresh.lock"))
	locked, err := fileLock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return RefreshResult{}, err
	}
	if !locked {
		if err := ctx.Err(); err != nil {
			return RefreshResult{}, err
		}
		return RefreshResult{}, errors.New("project refresh lock was not acquired")
	}

	reporter := report
	if report != nil {
		lastProgress := -1
		reporter = func(snapshot index.Progress) error {
			if snapshot.Percent < lastProgress {
				return nil
			}
			lastProgress = snapshot.Percent
			return report(snapshot)
		}
	}
	var result RefreshResult
	for attempt := 0; attempt < 2; attempt++ {
		result, err = p.refresh(ctx, reporter)
		if !errors.Is(err, errSourcesChanged) {
			break
		}
	}
	if err != nil {
		if stateErr := p.Store.SetMeta("index_state", "incomplete"); stateErr != nil {
			err = errors.Join(err, stateErr)
		}
	}
	return result, errors.Join(err, fileLock.Unlock())
}

var errSourcesChanged = index.ErrSourcesChanged

func (p *Project) refresh(ctx context.Context, report index.ProgressReporter) (RefreshResult, error) {
	var result RefreshResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	opt := index.Options{Ignore: p.Config.Ignore, Languages: p.Config.Languages}
	preSnapshot, err := index.SourceSnapshotWithOptionsContext(ctx, p.Workspace.Root, opt)
	if err != nil {
		return result, err
	}
	preSignature := preSnapshot.Signature
	generation, err := p.Store.BeginGeneration(ctx)
	if err != nil {
		return result, err
	}
	defer generation.Rollback()
	current := generation.Store
	if p.beforeIndex != nil {
		p.beforeIndex()
	}

	var summary index.Summary
	if report == nil {
		summary, err = index.IndexWithOptionsContext(ctx, current, p.Workspace.Root, opt)
	} else {
		summary, err = index.IndexWithOptionsProgressContext(ctx, current, p.Workspace.Root, opt, report)
	}
	if err != nil {
		return result, err
	}
	result.Summary = summary
	if p.afterIndex != nil {
		p.afterIndex()
	}
	if err := current.SetMeta("vectors_complete", "0"); err != nil {
		return result, err
	}
	if p.Inferencer != nil && inferencerEmbeds(p.Inferencer) {
		model := embedModelID(p.Inferencer, p.Config.AI.EmbedModel)
		result.Embedded, result.EmbeddingError = index.BuildVectors(ctx, current, p.Inferencer, model)
		if result.EmbeddingError != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if err := current.ResetVectors(); err != nil {
				return result, errors.Join(result.EmbeddingError, err)
			}
		} else {
			stored, err := current.GetMeta("embed_model")
			if err != nil {
				return result, err
			}
			if stored != model {
				return result, fmt.Errorf("vector model metadata = %q, want %q", stored, model)
			}
		}
	}
	sig, err := index.SignatureWithOptionsContext(ctx, p.Workspace.Root, opt)
	if err != nil {
		return result, err
	}
	if sig != preSignature {
		return result, errSourcesChanged
	}
	if err := index.ValidateSnapshotWithExpectedContext(ctx, current, p.Workspace.Root, opt, preSnapshot.Paths); err != nil {
		return result, errors.Join(errSourcesChanged, err)
	}
	if err := current.SetMeta("cli_sig", strconv.FormatUint(sig, 16)); err != nil {
		return result, err
	}
	if err := current.SetMeta("index_state", "complete"); err != nil {
		return result, err
	}
	if err := generation.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (p *Project) ensureFresh(ctx context.Context) (RefreshResult, error) {
	opt := index.Options{Ignore: p.Config.Ignore, Languages: p.Config.Languages}
	sig, sigErr := index.SignatureWithOptionsContext(ctx, p.Workspace.Root, opt)
	if sigErr != nil && ctx.Err() != nil {
		return RefreshResult{}, ctx.Err()
	}
	oldSig, _ := p.Store.GetMeta("cli_sig")
	oldVersion, _ := p.Store.GetMeta("index_version")
	indexState, _ := p.Store.GetMeta("index_state")
	vectorsComplete, _ := p.Store.GetMeta("vectors_complete")
	embedModel, _ := p.Store.GetMeta("embed_model")
	current := sigErr == nil && indexState == "complete" && oldVersion == index.Version() && oldSig == strconv.FormatUint(sig, 16)
	if p.Inferencer != nil && (vectorsComplete != "1" || embedModel != p.Config.AI.EmbedModel) {
		current = false
	}
	if current {
		return RefreshResult{}, nil
	}
	return p.Refresh(ctx)
}

// Close closes the project store once. Concurrent Close calls are safe; callers
// must stop active service operations before closing the project.
func (p *Project) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		p.refreshGate <- struct{}{}
		defer func() { <-p.refreshGate }()
		if p.Store != nil {
			p.closeErr = errors.Join(p.closeErr, p.Store.Close())
		}
	})
	return p.closeErr
}

func generationReadGuard(path string) store.ReadGuard {
	return func(ctx context.Context) (func(), error) {
		fileLock := flock.New(path)
		locked, err := fileLock.TryRLockContext(ctx, 25*time.Millisecond)
		if err != nil {
			return nil, err
		}
		if !locked {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, errors.New("project generation read lock was not acquired")
		}
		return func() { _ = fileLock.Unlock() }, nil
	}
}
