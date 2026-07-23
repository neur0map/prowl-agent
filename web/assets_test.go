package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestAssetsContainBuiltWorkbench(t *testing.T) {
	index, err := fs.ReadFile(Assets, "dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "Prowl Workbench") {
		t.Fatalf("embedded index does not identify workbench: %q", index)
	}
	javascript, err := fs.Glob(Assets, "dist/assets/*.js")
	if err != nil || len(javascript) != 1 {
		t.Fatalf("embedded javascript=%v err=%v", javascript, err)
	}
	styles, err := fs.Glob(Assets, "dist/assets/*.css")
	if err != nil || len(styles) != 1 {
		t.Fatalf("embedded styles=%v err=%v", styles, err)
	}
}
