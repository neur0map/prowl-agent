package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"
)

// outputFormat selects how query results are serialized for the CLI.
type outputFormat int

const (
	// formatTOON is the default: Token-Oriented Object Notation. Models read it
	// ~40% cheaper than JSON (and slightly more accurately) because uniform
	// arrays of objects collapse to a single header plus CSV-style rows, which
	// is exactly the shape of prowl's results (symbols, edges, chunks).
	formatTOON outputFormat = iota
	// formatJSON emits compact JSON, matching what the MCP server returns.
	formatJSON
	// formatHuman emits an explanatory terminal view with empty fields omitted.
	formatHuman
	// formatMarkdown emits a portable human view for reports and knowledge tools.
	formatMarkdown
)

func parseOutputFormat(value string) (outputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "human":
		return formatHuman, nil
	case "toon":
		return formatTOON, nil
	case "json":
		return formatJSON, nil
	case "markdown", "md":
		return formatMarkdown, nil
	default:
		return formatTOON, fmt.Errorf("unknown output format %q (choose human, toon, json, or markdown)", value)
	}
}

func defaultOutputFormat(terminal bool) outputFormat {
	if terminal {
		return formatHuman
	}
	return formatTOON
}

type outputOptions struct {
	name       string
	legacyJSON bool
}

func (o *outputOptions) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.name, "format", "", "output format: human, toon, json, or markdown (default: human on a terminal, toon when piped)")
	cmd.Flags().BoolVar(&o.legacyJSON, "json", false, "output JSON (shorthand for --format json)")
}

func (o outputOptions) resolve(w io.Writer) (outputFormat, error) {
	if o.legacyJSON {
		if o.name != "" && !strings.EqualFold(o.name, "json") {
			return formatTOON, fmt.Errorf("--json cannot be combined with --format %s", o.name)
		}
		return formatJSON, nil
	}
	if o.name != "" {
		return parseOutputFormat(o.name)
	}
	terminal := false
	if f, ok := w.(*os.File); ok {
		terminal = isTTY(f)
	}
	return defaultOutputFormat(terminal), nil
}

// formatValue serializes v in the requested format.
//
// Both formats go through encoding/json first, so the output honors the json
// struct tags already on every result type (snake_case names, omitempty, and
// json:"-" exclusions). TOON then re-encodes that generic JSON value, so the
// TOON and JSON outputs describe the identical data; TOON is just token-leaner.
func formatValue(v any, format outputFormat) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if format == formatJSON {
		return string(data), nil
	}
	if format == formatHuman || format == formatMarkdown {
		if overview, ok := v.(query.Overview); ok {
			return formatOverview(overview, format == formatMarkdown), nil
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return "", err
		}
		return formatReadable(generic, format == formatMarkdown), nil
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return "", err
	}
	return toon.MarshalString(generic)
}

func formatOverview(o query.Overview, markdown bool) string {
	var b strings.Builder
	if markdown {
		b.WriteString("# Project overview\n")
	} else {
		b.WriteString("Project overview\n")
		b.WriteString("================\n")
	}
	counts := make([]string, 0, 4)
	if o.Counts.Files > 0 {
		counts = append(counts, fmt.Sprintf("%d files", o.Counts.Files))
	}
	if o.Counts.Symbols > 0 {
		counts = append(counts, fmt.Sprintf("%d symbols", o.Counts.Symbols))
	}
	if o.Counts.Edges > 0 {
		counts = append(counts, fmt.Sprintf("%d relationships", o.Counts.Edges))
	}
	if o.Counts.Resources > 0 {
		counts = append(counts, fmt.Sprintf("%d resources", o.Counts.Resources))
	}
	if len(counts) == 0 {
		b.WriteString("\nNo indexable project content was found yet.\n")
	} else {
		b.WriteString("\n" + strings.Join(counts, " · ") + "\n")
	}
	writePaths := func(title string, paths []string) {
		if len(paths) == 0 {
			return
		}
		writeReadableHeading(&b, title, markdown)
		for _, path := range paths {
			if markdown {
				fmt.Fprintf(&b, "- `%s`\n", path)
			} else {
				fmt.Fprintf(&b, "  • %s\n", path)
			}
		}
	}
	writePaths("Start here", o.Docs)
	writePaths("Entrypoints", o.Entrypoints)
	if len(o.Clusters) > 0 {
		writeReadableHeading(&b, "Subsystems", markdown)
		for _, c := range o.Clusters {
			line := fmt.Sprintf("%s — %d files", c.Label, c.Files)
			if c.Lang != "" {
				line += " · " + c.Lang
			}
			if markdown {
				fmt.Fprintf(&b, "- **%s**\n", line)
			} else {
				fmt.Fprintf(&b, "  • %s\n", line)
			}
		}
	}
	if len(o.Hotspots) > 0 {
		writeReadableHeading(&b, "High-impact files", markdown)
		for _, h := range o.Hotspots {
			if markdown {
				fmt.Fprintf(&b, "- `%s` — %d dependents\n", h.File, h.In)
			} else {
				fmt.Fprintf(&b, "  • %s — %d dependents\n", h.File, h.In)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func writeReadableHeading(b *strings.Builder, title string, markdown bool) {
	if markdown {
		fmt.Fprintf(b, "\n## %s\n", title)
		return
	}
	fmt.Fprintf(b, "\n%s\n%s\n", title, strings.Repeat("-", len(title)))
}

func formatReadable(v any, markdown bool) string {
	var b strings.Builder
	renderReadable(&b, v, "Results", markdown, 0)
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "No results."
	}
	return out
}

func renderReadable(b *strings.Builder, v any, label string, markdown bool, depth int) {
	switch value := v.(type) {
	case nil:
		return
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key, item := range value {
			if !emptyReadable(item) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if depth == 0 && len(keys) > 0 {
			if markdown {
				fmt.Fprintf(b, "# %s\n", label)
			} else {
				fmt.Fprintf(b, "%s\n%s\n", label, strings.Repeat("=", len(label)))
			}
		}
		for _, key := range keys {
			item := value[key]
			title := readableTitle(key)
			switch item.(type) {
			case map[string]any, []any:
				writeReadableHeading(b, title, markdown)
				renderReadable(b, item, title, markdown, depth+1)
			default:
				fmt.Fprintf(b, "%s: %v\n", title, item)
			}
		}
	case []any:
		if len(value) == 0 {
			return
		}
		for i, item := range value {
			switch row := item.(type) {
			case map[string]any:
				fmt.Fprintf(b, "%d. ", i+1)
				first := true
				keys := make([]string, 0, len(row))
				for key, cell := range row {
					if !emptyReadable(cell) {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				for _, key := range keys {
					if !first {
						b.WriteString(" · ")
					}
					fmt.Fprintf(b, "%s: %v", readableTitle(key), row[key])
					first = false
				}
				b.WriteByte('\n')
			default:
				fmt.Fprintf(b, "- %v\n", item)
			}
		}
	default:
		fmt.Fprintln(b, value)
	}
}

func emptyReadable(v any) bool {
	switch value := v.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case []any:
		return len(value) == 0
	case map[string]any:
		return len(value) == 0
	case float64:
		return value == 0
	case bool:
		return !value
	default:
		return false
	}
}

func readableTitle(key string) string {
	words := strings.ReplaceAll(key, "_", " ")
	if words == "" {
		return words
	}
	return strings.ToUpper(words[:1]) + words[1:]
}
