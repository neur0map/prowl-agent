package cli

import (
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/selfupdate"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

func sampleStatus() query.Status {
	return query.Status{
		Counts: store.Counts{
			Files: 578, Symbols: 56535, Edges: 3383, Resolved: 34, Resources: 17,
			Langs: map[string]int{"cpp": 508, "bash": 21, "json": 16},
		},
		Savings: query.Savings{Queries: 100, AnswerTokens: 632000, SavedTokens: 3100000},
	}
}

func TestRenderStatusCard(t *testing.T) {
	out := renderStatusCard("v1", "/home/x/proj", "proj", sampleStatus(), selfupdate.Result{Available: true})
	for _, want := range []string{"prowl-agent", "proj", "INDEX", "LANGUAGES", "cpp", "TOKENS SAVED", "update available"} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q", want)
		}
	}
}

func TestPlainStatusShowsSavingsAndUpdate(t *testing.T) {
	var b strings.Builder
	printPlainStatus(&b, "/p", sampleStatus(), selfupdate.Result{Available: true})
	s := b.String()
	for _, want := range []string{"Files:", "Symbols:", "Saved:", "Update:"} {
		if !strings.Contains(s, want) {
			t.Errorf("plain status missing %q\n%s", want, s)
		}
	}
}

func TestHumanTokens(t *testing.T) {
	cases := map[int64]string{500: "500", 1500: "1.5k", 56535: "57k", 3100000: "3.1M"}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestComma(t *testing.T) {
	if got := comma(56535); got != "56,535" {
		t.Errorf("comma(56535) = %q", got)
	}
	if got := comma(7); got != "7" {
		t.Errorf("comma(7) = %q", got)
	}
	if got := comma(1000000); got != "1,000,000" {
		t.Errorf("comma(1000000) = %q", got)
	}
}
