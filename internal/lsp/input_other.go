//go:build !linux

package lsp

import (
	"context"
	"os"
	"sync"
)

// CancellableInput uses close-to-cancel on platforms without the Linux poll
// wake-pipe implementation. Platform release acceptance verifies this behavior.
type CancellableInput struct {
	file       *os.File
	cancelOnce sync.Once
}

func NewCancellableInput(_ context.Context, file *os.File) (*CancellableInput, error) {
	return &CancellableInput{file: file}, nil
}

func (r *CancellableInput) Read(p []byte) (int, error) { return r.file.Read(p) }

func (r *CancellableInput) CancelRead() error {
	var err error
	r.cancelOnce.Do(func() { err = r.file.Close() })
	return err
}

func (r *CancellableInput) Close() error { return nil }
