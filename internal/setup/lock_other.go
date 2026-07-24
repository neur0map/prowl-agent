//go:build !windows && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd

package setup

import (
	"context"
	"errors"
	"os"
	"time"
)

func lockSetupFile(_ context.Context, file *os.File, _ time.Duration) (func(), error) {
	_ = file.Close()
	return nil, errors.ErrUnsupported
}
