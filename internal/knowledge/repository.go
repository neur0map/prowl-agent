package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	indexStart = "<!-- prowl:index:start -->"
	indexEnd   = "<!-- prowl:index:end -->"
)

// Codec is implemented by a versioned interchange codec such as OKF v0.1.
type Codec interface {
	Parse(path string, data []byte) (*Document, error)
	Marshal(doc *Document) ([]byte, error)
}

// Repository owns canonical Markdown documents in one knowledge bundle.
type Repository struct {
	Root  string
	Codec Codec
}

func NewRepository(root string, codec Codec) *Repository {
	return &Repository{Root: root, Codec: codec}
}

// Init creates a valid empty bundle without overwriting existing human files.
func (r *Repository) Init() error {
	if r.Codec == nil {
		return errors.New("knowledge codec is required")
	}
	if err := os.MkdirAll(r.Root, 0o755); err != nil {
		return err
	}
	indexPath := filepath.Join(r.Root, "index.md")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		content := []byte("# Knowledge\n\n" + indexStart + "\n_No concepts yet._\n" + indexEnd + "\n")
		if err := atomicWrite(indexPath, content, 0o644); err != nil {
			return err
		}
	}
	logPath := filepath.Join(r.Root, "log.md")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		if err := atomicWrite(logPath, []byte("# Knowledge log\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// List parses all concept Markdown files, excluding reserved index and log files.
func (r *Repository) List() ([]*Document, error) {
	var docs []*Document
	err := filepath.WalkDir(r.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		base := strings.ToLower(entry.Name())
		if base == "index.md" || base == "log.md" {
			return nil
		}
		rel, err := filepath.Rel(r.Root, path)
		if err != nil {
			return err
		}
		doc, err := r.Read(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		docs = append(docs, doc)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, err
}

// Read parses a bundle-relative document after enforcing path containment.
func (r *Repository) Read(rel string) (*Document, error) {
	path, clean, err := r.resolve(rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return r.Codec.Parse(clean, data)
}

// Write validates and atomically writes a canonical document.
func (r *Repository) Write(doc *Document) error {
	if doc == nil {
		return errors.New("nil knowledge document")
	}
	path, clean, err := r.resolve(doc.Path)
	if err != nil {
		return err
	}
	doc.Path = clean
	data, err := r.Codec.Marshal(doc)
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, data, mode)
}

// Import copies and validates a Markdown document without modifying the source.
func (r *Repository) Import(source, destination string) (*Document, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	doc, err := r.Codec.Parse(filepath.ToSlash(destination), data)
	if err != nil {
		return nil, err
	}
	path, _, err := r.resolve(doc.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("knowledge destination already exists: %s", doc.Path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := r.Write(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// GenerateIndex refreshes only Prowl's marker-owned section in root index.md.
func (r *Repository) GenerateIndex() error {
	docs, err := r.List()
	if err != nil {
		return err
	}
	path := filepath.Join(r.Root, "index.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = []byte("# Knowledge\n")
	} else if err != nil {
		return err
	}
	var generated strings.Builder
	generated.WriteString(indexStart + "\n")
	if len(docs) == 0 {
		generated.WriteString("_No concepts yet._\n")
	} else {
		for _, doc := range docs {
			title := doc.Title
			if title == "" {
				title = strings.TrimSuffix(filepath.Base(doc.Path), filepath.Ext(doc.Path))
			}
			generated.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", title, doc.Path, doc.Type))
		}
	}
	generated.WriteString(indexEnd + "\n")
	updated := replaceOwnedBlock(data, []byte(generated.String()))
	return atomicWrite(path, updated, 0o644)
}

// AppendLog adds one UTC event using an atomic file replacement.
func (r *Repository) AppendLog(action, path string, at time.Time) error {
	logPath := filepath.Join(r.Root, "log.md")
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		data = []byte("# Knowledge log\n")
	} else if err != nil {
		return err
	}
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	line := fmt.Sprintf("- %s — %s `%s`\n", at.UTC().Format(time.RFC3339), action, filepath.ToSlash(path))
	return atomicWrite(logPath, append(data, []byte(line)...), 0o644)
}

// Export copies the complete bundle, including unknown files, to an empty path.
func (r *Repository) Export(destination string) error {
	if _, err := os.Stat(destination); err == nil {
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return readErr
		}
		if len(entries) != 0 {
			return fmt.Errorf("export destination is not empty: %s", destination)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return filepath.WalkDir(r.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.Root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return atomicWrite(target, data, info.Mode().Perm())
	})
}

func (r *Repository) resolve(rel string) (string, string, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel))))
	if clean == "" || clean == "." || filepath.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", fmt.Errorf("unsafe knowledge path %q", rel)
	}
	path := filepath.Join(r.Root, filepath.FromSlash(clean))
	return path, clean, nil
}

func replaceOwnedBlock(data, block []byte) []byte {
	start := bytes.Index(data, []byte(indexStart))
	if start >= 0 {
		endRel := bytes.Index(data[start:], []byte(indexEnd))
		if endRel >= 0 {
			end := start + endRel + len(indexEnd)
			if end < len(data) && data[end] == '\n' {
				end++
			}
			out := append(append([]byte(nil), data[:start]...), block...)
			return append(out, data[end:]...)
		}
	}
	out := append([]byte(nil), data...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return append(out, block...)
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".prowl-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
