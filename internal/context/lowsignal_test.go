package context

import "testing"

func TestLowSignalClass(t *testing.T) {
	cases := []struct{ path, want string }{
		{"internal/query/query.go", ""},
		{"widgets/Battery.qml", ""},
		{"cmd/main.go", ""},
		{"package-lock.json", "lockfile"},
		{"go.sum", "lockfile"},
		{"Cargo.lock", "lockfile"},
		{"deps/gradle.lockfile", "lockfile"},
		{"web/dist/app.min.js", "minified"},
		{"assets/app.css.map", "minified"},
		{"api/user.pb.go", "generated"},
		{"internal/store/schema_gen.go", "generated"},
		{"src/generated/models.ts", "generated"},
		{"generated/api.go", "generated"},
		{"translations/es.json", "locale"},
		{"app/i18n/en.json", "locale"},
		{"src/langs/pt.json", "locale"},
		{"po/de.po", "locale"},
		{"locale/fr/app.arb", "locale"},
	}
	for _, c := range cases {
		if got := lowSignalClass(c.path); got != c.want {
			t.Errorf("lowSignalClass(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestQueryWantsClass(t *testing.T) {
	if !queryWantsClass([]string{"spanish", "translation"}, "locale") {
		t.Error("a translation query should want the locale class")
	}
	if queryWantsClass([]string{"battery", "status"}, "locale") {
		t.Error("a battery query should not want the locale class")
	}
	if !queryWantsClass([]string{"dependency", "lock"}, "lockfile") {
		t.Error("a lock query should want the lockfile class")
	}
	if !queryWantsClass([]string{"protobuf"}, "generated") {
		t.Error("a protobuf query should want the generated class")
	}
}

func TestRankDownweightsLowSignal(t *testing.T) {
	// A keyword-dense locale file with a higher lexical score than the real
	// code must rank below the real code once it is marked low-signal.
	candidates := []Candidate{
		{Item: Item{ID: "locale", Freshness: "current", Confidence: 1}, LexicalScore: 90, LowSignal: true, LowSignalClass: "locale"},
		{Item: Item{ID: "code", Freshness: "current", Confidence: 1}, LexicalScore: 45},
	}
	ranked := RankCandidates(candidates)
	if ranked[0].ID != "code" {
		t.Fatalf("expected real code first, got %q then %q", ranked[0].ID, ranked[1].ID)
	}

	// A direct identifier match to a low-signal file is not dampened.
	direct := []Candidate{
		{Item: Item{ID: "locale", Freshness: "current", Confidence: 1}, LexicalScore: 5, LowSignal: true, LowSignalClass: "locale", DirectMatch: true},
		{Item: Item{ID: "code", Freshness: "current", Confidence: 1}, LexicalScore: 45},
	}
	if ranked := RankCandidates(direct); ranked[0].ID != "locale" {
		t.Fatalf("direct-match low-signal should rank first, got %q", ranked[0].ID)
	}
}

func TestLowSignalCaseSurfacesRealCode(t *testing.T) {
	fixture := newEvaluationFixture(t)
	defer fixture.store.Close()
	var target retrievalCase
	for _, c := range fixture.cases {
		if c.Name == "battery display low-signal" {
			target = c
		}
	}
	if target.Name == "" {
		t.Fatal("battery low-signal case missing from corpus")
	}
	packet, err := fixture.service.Search(Request{Question: target.Question, Mode: ModeCompact, BudgetTokens: target.BudgetTokens})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, item := range packet.Items {
		titles = append(titles, item.Title)
	}
	t.Logf("selected=%v tokens=%d omitted=%v", titles, packet.Budget.EstimatedTokens, packet.Omitted)
	metrics := scoreEvaluation(target, packetEvaluation(packet))
	if metrics.HitRate < 1 {
		t.Fatalf("real code bar/battery.go not surfaced: recall=%.2f", metrics.HitRate)
	}
	if metrics.Precision < 1 {
		t.Fatalf("low-signal distractor i18n/catalog_gen.go selected: precision=%.2f", metrics.Precision)
	}
}

func TestLowSignalClassDocs(t *testing.T) {
	for _, p := range []string{"README.md", "docs/guide.md", "CHANGELOG.md", "notes.rst", "api.mdx"} {
		if got := lowSignalClass(p); got != "docs" {
			t.Errorf("lowSignalClass(%q) = %q, want docs", p, got)
		}
	}
	if got := lowSignalClass("internal/query/query.go"); got != "" {
		t.Errorf("a source file must not be docs, got %q", got)
	}
	if !queryWantsClass([]string{"architecture", "guide"}, "docs") {
		t.Error("a guide query should keep docs at full weight")
	}
	if queryWantsClass([]string{"battery", "status"}, "docs") {
		t.Error("a code query should not keep docs at full weight")
	}
}
