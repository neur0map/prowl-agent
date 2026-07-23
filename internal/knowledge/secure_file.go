package knowledge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MaxDocumentBytes bounds canonical Markdown and proposal candidates. Knowledge
// documents are intended for progressive context, not arbitrary binary storage.
const MaxDocumentBytes int64 = 4 << 20

func readRootFile(rootPath, relativePath string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(relativePath))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBoundedRegular(file, relativePath, limit)
}

func readRegularFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBoundedRegular(file, path, limit)
}

func readBoundedRegular(file *os.File, label string, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("knowledge file is not regular: %s", label)
	}
	if limit > 0 && info.Size() > limit {
		return nil, fmt.Errorf("knowledge file exceeds %d bytes: %s", limit, label)
	}
	reader := io.Reader(file)
	if limit > 0 {
		reader = io.LimitReader(file, limit+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, fmt.Errorf("knowledge file exceeds %d bytes: %s", limit, label)
	}
	return data, nil
}
