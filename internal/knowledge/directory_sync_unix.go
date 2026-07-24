//go:build !windows

package knowledge

import "os"

func syncDirectoryAfterRename(directory *os.File) error {
	return directory.Sync()
}
