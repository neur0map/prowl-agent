//go:build windows

package setup

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const setupLockViolation windows.Errno = 0x21

func lockSetupFile(ctx context.Context, file *os.File, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		err := windows.LockFileEx(
			windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			&windows.Overlapped{},
		)
		if err == nil || errors.Is(err, windows.Errno(0)) {
			return func() {
				_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, setupLockViolation) && !errors.Is(err, windows.ERROR_IO_PENDING) {
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
