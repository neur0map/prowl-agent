//go:build windows

package knowledge

import "testing"

func TestSyncDirectoryAfterRenameSkipsUnsupportedWindowsDirectoryFlush(t *testing.T) {
	if err := syncDirectoryAfterRename(nil); err != nil {
		t.Fatal(err)
	}
}
