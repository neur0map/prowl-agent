//go:build !windows

package setup

import "os"

func syncSetupDirectory(directory *os.File) error {
	return directory.Sync()
}
