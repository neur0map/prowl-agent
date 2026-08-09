package docs

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The llms.txt convention (https://llmstxt.org) lets a docs site publish an
// agent-friendly entry point: llms-full.txt is the entire documentation as one
// Markdown file, and llms.txt is a curated Markdown index of the pages worth
// reading. Preferring these avoids crawling navigation chrome and is far cheaper
// for an agent than fetching many HTML pages, so the crawler tries them first.

const minLLMSFullBytes = 2000 // ignore stub/placeholder llms-full.txt files

var mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\((https?://[^)\s]+)\)`)

// tryLLMSFull fetches <host>/llms-full.txt. It returns the Markdown and true
// only when the file exists, is substantial, and carries no prompt injection.
func tryLLMSFull(ctx context.Context, client *http.Client, start *url.URL, ua string) (string, bool) {
	u := &url.URL{Scheme: start.Scheme, Host: start.Host, Path: "/llms-full.txt"}
	body, ctype, ok := fetch(ctx, client, u, ua)
	if !ok || strings.Contains(ctype, "text/html") || len(body) < minLLMSFullBytes {
		return "", false
	}
	md := string(body)
	if looksLikeInjection(md) {
		return "", false
	}
	return md, true
}

// tryLLMSIndex fetches <host>/llms.txt and returns the absolute, same-host links
// it lists. These are a curated seed set the crawler fetches directly instead of
// discovering pages by breadth-first traversal.
func tryLLMSIndex(ctx context.Context, client *http.Client, start *url.URL, ua string) ([]string, bool) {
	u := &url.URL{Scheme: start.Scheme, Host: start.Host, Path: "/llms.txt"}
	body, ctype, ok := fetch(ctx, client, u, ua)
	if !ok || strings.Contains(ctype, "text/html") {
		return nil, false
	}
	seen := map[string]bool{}
	var links []string
	for _, m := range mdLinkRe.FindAllStringSubmatch(string(body), -1) {
		ref, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		abs := start.ResolveReference(ref)
		abs.Fragment = ""
		if abs.Host != start.Host {
			continue
		}
		s := abs.String()
		if !seen[s] {
			seen[s] = true
			links = append(links, s)
		}
	}
	return links, len(links) > 0
}

// writeLLMSFull stores llms-full.txt as a single indexed page.
func writeLLMSFull(cfg CrawlConfig, md string) (CrawlStats, error) {
	src := &url.URL{Scheme: cfg.Start.Scheme, Host: cfg.Start.Host, Path: "/llms-full.txt"}
	pg := page{title: cfg.Start.Host + " (llms-full)", markdown: strings.TrimSpace(md)}
	if err := writePage(cfg.OutputDir, src, pg, false); err != nil {
		return CrawlStats{}, err
	}
	if cfg.Progress != nil {
		cfg.Progress(1, 0, src.String())
	}
	return CrawlStats{Pages: 1}, nil
}

// crawlSeeds fetches a curated list of URLs directly (no link discovery),
// applying the same rate limiting, robots rules, injection quarantine, and
// Markdown conversion as the breadth-first crawler.
func crawlSeeds(ctx context.Context, client *http.Client, robots robotsRules, cfg CrawlConfig, links []string) (CrawlStats, error) {
	interval := time.Duration(float64(time.Second) / cfg.Rate)
	visited := map[string]bool{}
	var stats CrawlStats
	for _, l := range links {
		if stats.Pages >= cfg.MaxPages {
			break
		}
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		u, err := url.Parse(l)
		if err != nil || visited[u.String()] {
			continue
		}
		visited[u.String()] = true
		if robots.disallowed(u.Path) {
			continue
		}
		time.Sleep(interval)
		body, ok := fetchHTML(ctx, client, u, cfg.UserAgent)
		if !ok {
			continue
		}
		pg, err := extractPage(body, u)
		if err != nil || strings.TrimSpace(pg.markdown) == "" {
			continue
		}
		if looksLikeInjection(pg.markdown) {
			_ = writePage(cfg.QuarantineDir, u, pg, true)
			stats.Quarantined++
		} else if writePage(cfg.OutputDir, u, pg, false) == nil {
			stats.Pages++
		}
		if cfg.Progress != nil {
			cfg.Progress(stats.Pages, stats.Quarantined, u.String())
		}
	}
	return stats, nil
}
