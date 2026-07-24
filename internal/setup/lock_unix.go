//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

package setup

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func lockSetupFile(ctx context.Context, file *os.File, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = file.Close()
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		wait := min(10*time.Millisecond, time.Until(deadline))
		if wait <= 0 {
			_ = file.Close()
			return nil, errors.New("setup apply is already in progress")
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
