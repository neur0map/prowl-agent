package docs

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// robotsRules holds the Disallow prefixes that apply to our crawler, gathered
// from robots.txt groups matching either "*" or our user-agent token.
type robotsRules struct {
	disallow []string
}

func (r robotsRules) disallowed(path string) bool {
	for _, d := range r.disallow {
		if d == "/" {
			return true
		}
		if d != "" && strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// fetchRobots retrieves and parses robots.txt for start's host. A missing or
// unreadable robots.txt yields no restrictions (permissive default).
func fetchRobots(ctx context.Context, client *http.Client, start *url.URL, ua string) robotsRules {
	ru := &url.URL{Scheme: start.Scheme, Host: start.Host, Path: "/robots.txt"}
	body, _, ok := fetch(ctx, client, ru, ua)
	if !ok {
		return robotsRules{}
	}
	return parseRobots(string(body), ua)
}

// parseRobots collects Disallow rules from groups whose User-agent is "*" or a
// token contained in ua (case-insensitive). This is a deliberately small subset
// of the robots spec: enough to be polite, not a full matcher.
func parseRobots(body, ua string) robotsRules {
	uaLower := strings.ToLower(ua)
	var rules robotsRules
	var groupApplies bool
	var sawAgentInGroup bool

	for _, raw := range strings.Split(body, "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			// A new user-agent line after a rule line starts a new group.
			if sawAgentInGroup {
				groupApplies = false
			}
			sawAgentInGroup = true
			token := strings.ToLower(val)
			if token == "*" || (token != "" && strings.Contains(uaLower, token)) {
				groupApplies = true
			}
		case "disallow":
			sawAgentInGroup = false
			if groupApplies {
				rules.disallow = append(rules.disallow, val)
			}
		case "allow":
			sawAgentInGroup = false
		}
	}
	return rules
}
