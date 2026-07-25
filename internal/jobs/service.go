package jobs

import (
	"context"
	"errors"
	"sync"

	"github.com/gofrs/flock"
)

// Publisher is the narrow post-commit delivery boundary used by the worker.
type Publisher interface {
	PublishCommitted(context.Context) error
	Close() error
}

// Runner performs an index refresh and reports bounded durable progress.
type Runner func(context.Context, Job, func(string, int) error) error

var ErrWorkerActive = errors.New("jobs worker is already active")

type Service struct {
	store      *Store
	publisher  Publisher
	runner     Runner
	ctx        context.Context
	cancel     context.CancelFunc
	notify     chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once
	wg         sync.WaitGroup
	startErr   error
	closeErr   error
	workerLock *flock.Flock
	// beforeClaim is a package-private deterministic worker test seam.
	beforeClaim func()
}

func NewService(store *Store, publisher Publisher, runner Runner) *Service {
	return &Service{store: store, publisher: publisher, runner: runner, notify: make(chan struct{}, 1)}
}
func (s *Service) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		if s.store == nil || s.runner == nil {
			s.startErr = ErrInvalidJob
			return
		}
		lock := flock.New(s.store.Path() + ".worker.lock")
		locked, err := lock.TryLock()
		if err != nil {
			s.startErr = err
			return
		}
		if !locked {
			s.startErr = ErrWorkerActive
			return
		}
		s.workerLock = lock
		s.ctx, s.cancel = context.WithCancel(ctx)
		if err := s.store.reconcile(s.ctx); err != nil {
			s.startErr = errors.Join(err, s.releaseWorkerLock())
			s.cancel()
			return
		}
		s.wg.Add(1)
		go s.work()
		s.signal()
	})
	return s.startErr
}
func (s *Service) EnqueueOrResumeIndex(ctx context.Context) (Job, bool, error) {
	job, created, err := s.store.EnqueueOrResumeIndex(ctx)
	if err == nil {
		s.publish(ctx)
		s.signal()
	}
	return job, created, err
}
func (s *Service) Get(ctx context.Context, id string) (Job, error) { return s.store.Get(ctx, id) }

// StreamState returns the current durable project-job stream authority.
func (s *Service) StreamState(ctx context.Context) (StreamState, error) {
	if s == nil || s.store == nil {
		return StreamState{}, ErrClosed
	}
	return s.store.State(ctx)
}

// Snapshot returns a job and the stream head from one durable read snapshot.
func (s *Service) Snapshot(ctx context.Context, id string) (Job, StreamState, error) {
	if s == nil || s.store == nil {
		return Job{}, StreamState{}, ErrClosed
	}
	return s.store.Snapshot(ctx, id)
}
func (s *Service) Cancel(ctx context.Context, id string, version uint64) (Job, error) {
	job, err := s.store.Cancel(ctx, id, version)
	if err == nil {
		s.publish(ctx)
		s.signal()
	}
	return job, err
}
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
		s.closeErr = errors.Join(s.closeErr, s.releaseWorkerLock())
		if s.publisher != nil {
			s.closeErr = errors.Join(s.closeErr, s.publisher.Close())
		}
		if s.store != nil {
			s.closeErr = errors.Join(s.closeErr, s.store.Close())
		}
	})
	return s.closeErr
}

func (s *Service) releaseWorkerLock() error {
	if s.workerLock == nil {
		return nil
	}
	err := s.workerLock.Unlock()
	s.workerLock = nil
	return err
}
func (s *Service) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
func (s *Service) publish(ctx context.Context) {
	if s.publisher != nil {
		_ = s.publisher.PublishCommitted(ctx)
	}
}
func (s *Service) work() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		if s.beforeClaim != nil {
			s.beforeClaim()
		}
		job, ok, err := s.store.claim(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}
		if !ok {
			select {
			case <-s.ctx.Done():
				return
			case <-s.notify:
			}
			continue
		}
		s.publish(s.ctx)
		if job.Status == StatusCancelled {
			continue
		}
		runCtx, cancel := context.WithCancel(s.ctx)
		s.store.setActive(job.ID, cancel)
		current, getErr := s.store.Get(runCtx, job.ID)
		if getErr != nil {
			err = getErr
		} else if current.Status == StatusCancelling {
			cancel()
			err = context.Canceled
		} else if err = runCtx.Err(); err == nil {
			err = s.runner(runCtx, job, func(phase string, progress int) error {
				_, err := s.store.updateProgress(runCtx, job.ID, phase, progress)
				if err == nil {
					s.publish(runCtx)
				}
				return err
			})
		}
		cancel()
		s.store.clearActive(job.ID)
		if s.ctx.Err() != nil {
			return
		}
		if _, finishErr := s.store.finish(context.Background(), job.ID, err); finishErr == nil {
			s.publish(s.ctx)
		}
	}
}
