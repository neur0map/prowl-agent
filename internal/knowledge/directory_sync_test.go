package knowledge

import (
	"os"
	"testing"
)

func TestSyncDirectoryAfterRenameSucceeds(t *testing.T) {
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := syncDirectoryAfterRename(directory); err != nil {
		t.Fatal(err)
	}
}
