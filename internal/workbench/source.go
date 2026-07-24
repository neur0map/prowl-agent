package workbench

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/boundedio"
)

const (
	MaxSourcePreviewLines         = 400
	MaxSourcePreviewBytes         = 128 << 10
	MaxSourcePreviewResponseBytes = 1 << 20
	maxSourcePreviewPath          = 4096
)

var (
	ErrInvalidSourcePreview = errors.New("invalid source preview request")
	ErrSourceNotFound       = errors.New("source preview is unavailable")
	ErrSourceTooLarge       = errors.New("source preview exceeds bounds")
)

// SourcePreviewRequest identifies one bounded, project-relative source range.
type SourcePreviewRequest struct {
	Path      string
	LineStart int
	LineEnd   int
}

// SourceLine is one exact source line in a preview.
type SourceLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// SourcePreview contains a bounded, line-addressable source range.
type SourcePreview struct {
	Path            string       `json:"path"`
	LineStart       int          `json:"line_start"`
	LineEnd         int          `json:"line_end"`
	Lines           []SourceLine `json:"lines"`
	resourceVersion string
}

// SourcePreview reads an indexed source file through a rooted, regular-file-only descriptor.
func (service *Service) SourcePreview(ctx context.Context, request SourcePreviewRequest) (SourcePreview, error) {
	if err := request.validate(); err != nil {
		return SourcePreview{}, err
	}
	if service == nil || service.project == nil || service.project.Workspace == nil || service.project.Store == nil {
		return SourcePreview{}, ErrSourceNotFound
	}
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return SourcePreview{}, err
	}
	defer release()
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		return SourcePreview{}, err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		return SourcePreview{}, err
	}
	if _, found, err := service.project.Store.GetFileByPath(request.Path); err != nil {
		return SourcePreview{}, err
	} else if !found {
		return SourcePreview{}, ErrSourceNotFound
	}

	root, err := os.OpenRoot(service.project.Workspace.Root)
	if err != nil {
		return SourcePreview{}, ErrSourceNotFound
	}
	defer root.Close()
	file, err := boundedio.OpenRegular(root, filepath.FromSlash(request.Path))
	if err != nil {
		return SourcePreview{}, ErrSourceNotFound
	}
	defer file.Close()
	lines, err := readPreviewLines(ctx, file, request.LineStart, request.LineEnd)
	if err != nil {
		return SourcePreview{}, err
	}
	return SourcePreview{Path: request.Path, LineStart: request.LineStart, LineEnd: request.LineEnd, Lines: lines, resourceVersion: version}, nil
}

func (request SourcePreviewRequest) validate() error {
	if request.LineStart < 1 || request.LineEnd < request.LineStart || request.LineEnd-request.LineStart+1 > MaxSourcePreviewLines {
		return ErrInvalidSourcePreview
	}
	if request.Path == "" || len(request.Path) > maxSourcePreviewPath || !utf8.ValidString(request.Path) || strings.IndexFunc(request.Path, unicode.IsControl) >= 0 || strings.Contains(request.Path, "\\") {
		return ErrInvalidSourcePreview
	}
	if filepath.IsAbs(request.Path) || path.IsAbs(request.Path) || (len(request.Path) >= 2 && request.Path[1] == ':' && ((request.Path[0] >= 'A' && request.Path[0] <= 'Z') || (request.Path[0] >= 'a' && request.Path[0] <= 'z'))) {
		return ErrInvalidSourcePreview
	}
	clean := path.Clean(request.Path)
	if clean != request.Path || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ErrInvalidSourcePreview
	}
	return nil
}

func readPreviewLines(ctx context.Context, file *os.File, lineStart, lineEnd int) ([]SourceLine, error) {
	scanner := bufio.NewScanner(io.LimitReader(file, MaxSourcePreviewBytes+1))
	scanner.Buffer(make([]byte, 64<<10), MaxSourcePreviewBytes+1)
	lines := make([]SourceLine, 0, lineEnd-lineStart+1)
	var outputBytes int
	for number := 1; scanner.Scan(); number++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if number < lineStart {
			continue
		}
		text := scanner.Text()
		outputBytes += len(text) + 1
		if outputBytes > MaxSourcePreviewBytes {
			return nil, ErrSourceTooLarge
		}
		lines = append(lines, SourceLine{Number: number, Text: text})
		if number == lineEnd {
			return lines, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSourceTooLarge, err)
	}
	return nil, ErrInvalidSourcePreview
}
