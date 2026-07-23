//go:build linux

package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLSPRunCancellationInterruptsInheritedStdin(t *testing.T) {
	if os.Getenv("PROWL_LSP_STDIN_HELPER") == "1" {
		ctx, cancel := context.WithCancel(context.Background())
		input, err := NewCancellableInput(ctx, os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		defer input.Close()
		time.AfterFunc(100*time.Millisecond, cancel)
		err = (&Server{}).Run(ctx, input, io.Discard)
		if !errors.Is(err, context.Canceled) {
			os.Exit(3)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLSPRunCancellationInterruptsInheritedStdin$")
	cmd.Env = append(os.Environ(), "PROWL_LSP_STDIN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_ = stdin.Close()
		if err != nil {
			t.Fatalf("inherited-stdin helper: %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		<-done
		t.Fatal("Run remained blocked on canceled inherited stdin")
	}
}

func TestLSPRunNormalEOF(t *testing.T) {
	if err := (&Server{}).Run(context.Background(), &eofReadCloser{}, io.Discard); err != nil {
		t.Fatalf("Run EOF error = %v", err)
	}
}

type eofReadCloser struct{}

func (*eofReadCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (*eofReadCloser) Close() error             { return nil }
