//go:build windows

package setup

import "os"

// Directory handles cannot be flushed on Windows; the replacement file has
// already been synced before it is renamed.
func syncSetupDirectory(_ *os.File) error {
	return nil
}
