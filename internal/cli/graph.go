package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

// maxGraphNodes caps how many files the interactive graph draws, so a huge repo
// stays smooth; the most-connected files are kept.
const maxGraphNodes = 800

type graphNode struct {
	ID   int    `json:"id"`
	Path string `json:"p"`
	Sub  string `json:"s"`
	Deg  int    `json:"d"`
}

type graphLink struct {
	S int `json:"s"`
	T int `json:"t"`
}

type graphData struct {
	Nodes []graphNode `json:"nodes"`
	Links []graphLink `json:"links"`
}

// newGraphCmd writes a self-contained, interactive dependency graph of the repo
// to an HTML file: files are nodes (colored by subsystem, sized by how many
// files depend on them), resolved file-to-file edges are links. It is a single
// file with no external assets, so it opens offline in any browser -- a visual
// map of the codebase to hand a human or screenshot for a review.
func newGraphCmd() *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "graph",
		Short: "Write an interactive HTML dependency graph of the repo (files as nodes, resolved edges as links)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, ws, s, closer, err := openQuerier(cmd.Context(), false)
			if err != nil {
				return err
			}
			defer closer()
			data, err := buildGraphData(s)
			if err != nil {
				return err
			}
			if len(data.Nodes) == 0 {
				return fmt.Errorf("no resolved file dependencies to graph yet (try prowl-agent init)")
			}
			path := out
			if path == "" {
				path = filepath.Join(ws.Path, "graph.html")
			}
			html, err := renderGraphHTML(filepath.Base(ws.Root), data)
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d nodes, %d links to %s\nopen it in a browser: file://%s\n",
				len(data.Nodes), len(data.Links), path, path)
			return nil
		},
	}
	c.Flags().StringVar(&out, "html", "", "output path (default: .prowl/graph.html)")
	return c
}

// buildGraphData turns the resolved file-to-file dependency edges into nodes and
// links: only connected files become nodes, deduped edges become links, and a
// node's degree is how many files depend on it.
func buildGraphData(s *store.Store) (graphData, error) {
	edges, err := s.FileDepEdges()
	if err != nil {
		return graphData{}, err
	}
	files, err := s.AllFiles()
	if err != nil {
		return graphData{}, err
	}
	lang := make(map[string]string, len(files))
	for _, f := range files {
		lang[f.RelPath] = f.Lang
	}
	type pair struct{ a, b string }
	seen := map[pair]bool{}
	inDeg := map[string]int{}
	connected := map[string]bool{}
	var pairs []pair
	for _, e := range edges {
		if e.SrcFile == "" || e.DstFile == "" || e.SrcFile == e.DstFile {
			continue
		}
		p := pair{e.SrcFile, e.DstFile}
		if seen[p] {
			continue
		}
		seen[p] = true
		pairs = append(pairs, p)
		inDeg[e.DstFile]++
		connected[e.SrcFile] = true
		connected[e.DstFile] = true
	}
	// Keep the most-connected nodes when a repo is very large.
	paths := make([]string, 0, len(connected))
	for p := range connected {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if inDeg[paths[i]] != inDeg[paths[j]] {
			return inDeg[paths[i]] > inDeg[paths[j]]
		}
		return paths[i] < paths[j]
	})
	if len(paths) > maxGraphNodes {
		paths = paths[:maxGraphNodes]
	}
	id := make(map[string]int, len(paths))
	data := graphData{}
	for _, p := range paths {
		id[p] = len(data.Nodes)
		data.Nodes = append(data.Nodes, graphNode{ID: id[p], Path: p, Sub: subsystemOf(p), Deg: inDeg[p]})
	}
	for _, p := range pairs {
		si, sok := id[p.a]
		ti, tok := id[p.b]
		if sok && tok {
			data.Links = append(data.Links, graphLink{S: si, T: ti})
		}
	}
	return data, nil
}

// subsystemOf groups a file by its top two path segments (internal/cli), or the
// first segment, so nodes color by subsystem.
func subsystemOf(path string) string {
	parts := strings.Split(path, "/")
	switch {
	case len(parts) >= 2:
		return parts[0] + "/" + parts[1]
	case len(parts) == 1:
		return "(root)"
	default:
		return parts[0]
	}
}

// renderGraphHTML embeds the graph data into the self-contained HTML template.
func renderGraphHTML(title string, data graphData) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	html := strings.Replace(graphHTMLTemplate, "/*PROWL_TITLE*/", jsonString(title), 1)
	html = strings.Replace(html, "/*PROWL_DATA*/", string(payload), 1)
	return html, nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
