//go:build linux

package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// CancellableInput makes an inherited file descriptor interruptible without
// closing it from another goroutine. CancelRead wakes poll through a private
// pipe; Close releases only that pipe and never closes the caller's file.
type CancellableInput struct {
	ctx        context.Context
	file       *os.File
	wakeRead   int
	wakeWrite  int
	cancelOnce sync.Once
	closeOnce  sync.Once
}

// NewCancellableInput wraps stdin or another Unix file for context-aware reads.
func NewCancellableInput(ctx context.Context, file *os.File) (*CancellableInput, error) {
	wake := []int{0, 0}
	if err := unix.Pipe2(wake, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, err
	}
	return &CancellableInput{ctx: ctx, file: file, wakeRead: wake[0], wakeWrite: wake[1]}, nil
}

func (r *CancellableInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	pollFDs := []unix.PollFd{
		{Fd: int32(r.file.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
		{Fd: int32(r.wakeRead), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
	}
	for {
		_, err := unix.Poll(pollFDs, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			if ctxErr := r.ctx.Err(); ctxErr != nil {
				return 0, ctxErr
			}
			return 0, err
		}
		if pollFDs[1].Revents != 0 {
			if err := r.ctx.Err(); err != nil {
				return 0, err
			}
			return 0, os.ErrClosed
		}
		if pollFDs[0].Revents != 0 {
			n, readErr := unix.Read(int(r.file.Fd()), p)
			if errors.Is(readErr, unix.EINTR) || errors.Is(readErr, unix.EAGAIN) {
				continue
			}
			if n == 0 && readErr == nil {
				return 0, io.EOF
			}
			return n, readErr
		}
	}
}

// CancelRead wakes an in-progress poll. It is safe to call more than once.
func (r *CancellableInput) CancelRead() error {
	var err error
	r.cancelOnce.Do(func() {
		for {
			_, err = unix.Write(r.wakeWrite, []byte{1})
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if errors.Is(err, unix.EAGAIN) {
				err = nil
			}
			break
		}
	})
	return err
}

// Close releases the private wake pipe without closing the wrapped file.
func (r *CancellableInput) Close() error {
	var err error
	r.closeOnce.Do(func() {
		err = errors.Join(unix.Close(r.wakeRead), unix.Close(r.wakeWrite))
	})
	return err
}
