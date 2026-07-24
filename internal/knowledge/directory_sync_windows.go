//go:build windows

package knowledge

import "os"

// syncDirectoryAfterRename is intentionally a no-op on Windows: directory handles
// cannot be flushed there, while the replacement file has already been synced.
func syncDirectoryAfterRename(_ *os.File) error {
	return nil
}
