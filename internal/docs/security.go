package docs

import (
	"regexp"
	"strings"
)

// injectionPatterns match imperative prompt-injection directives aimed at an AI
// reader. They are intentionally conservative: each requires an override verb
// together with an instruction/prompt/system target, so ordinary prose (even a
// security page that merely mentions "prompt injection") is not flagged, but a
// page that actually tries to command the agent is. Crawled documentation is
// untrusted content flowing into an agent's context, so flagged pages are
// quarantined out of the searchable corpus rather than indexed.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+|the\s+|any\s+)?(previous|prior|above|earlier)\s+(instructions|prompts?|messages?|context)`),
	regexp.MustCompile(`(?i)disregard\s+(all\s+|the\s+|any\s+)?(previous|prior|above|earlier|system)\s+(instructions|prompts?|rules?)`),
	regexp.MustCompile(`(?i)forget\s+(all\s+|everything\s+)?(previous|prior|above|your)\s+(instructions|prompts?|rules?|training)`),
	regexp.MustCompile(`(?i)(reveal|print|repeat|show|output)\s+(me\s+)?(your|the)\s+(system\s+prompt|instructions|initial\s+prompt)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an|in|no\s+longer)`),
	regexp.MustCompile(`(?i)new\s+(system\s+)?(instructions?|directive|prompt)\s*[:\-]`),
	regexp.MustCompile(`(?i)(override|bypass|ignore)\s+(all\s+)?(safety|security|content)\s+(policies|guidelines|filters?|rules?)`),
}

// looksLikeInjection reports whether text contains a prompt-injection directive.
func looksLikeInjection(text string) bool {
	// Bound the scan; injections live in visible prose, not megabytes of it.
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	lower := strings.ToLower(text)
	for _, re := range injectionPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}
