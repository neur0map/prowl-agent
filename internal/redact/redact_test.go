package redact

import (
	"strings"
	"testing"
)

func TestTextMasksProviderCredentials(t *testing.T) {
	cases := []struct{ name, in, leak string }{
		{"stripe", `STRIPE_TOKEN = "sk_live_` + `51H8xQ2eZvKYlo2CabcdefghijklmnopQRST"`, "sk_live_" + "51H8xQ2eZvKYlo2C"},
		{"openai", `key = "sk-proj-` + `1234567890abcdefGHIJKLMNOPqrstuvwxyz0987654321"`, "sk-proj-" + "1234567890abcdef"},
		{"github", `token: ghp_` + `1234567890abcdefghijklmnopqrstuvwx`, "ghp_" + "1234567890abcdef"},
		{"aws id", `AWS_ACCESS_KEY_ID=AKIA` + `IOSFODNN7EXAMPLE`, "AKIA" + "IOSFODNN7EXAMPLE"},
		{"url creds", `DATABASE_URL=postgres://admin:sup3rs3cr3tpw@db.internal:5432/prod`, "sup3rs3cr3tpw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := Text(c.in)
			if n == 0 {
				t.Fatalf("nothing masked in %q", c.in)
			}
			if strings.Contains(got, c.leak) {
				t.Fatalf("secret survived masking: %q", got)
			}
			if !strings.Contains(got, Mask) {
				t.Fatalf("no mask marker in %q", got)
			}
		})
	}
}

// The identifier must survive so retrieval still answers "where is this set".
func TestTextKeepsSurroundingCode(t *testing.T) {
	got, n := Text(`STRIPE_TOKEN = "sk_live_` + `51H8xQ2eZvKYlo2CabcdefghijklmnopQRST"`)
	if n != 1 {
		t.Fatalf("masked %d values, want 1", n)
	}
	if !strings.Contains(got, "STRIPE_TOKEN") {
		t.Fatalf("identifier lost: %q", got)
	}
}

// False positives are the real risk: masking ordinary code would hide the very
// lines an agent is looking for. Under R13 the generic heuristic is gone, so
// every one of these -- including secret-named prose, paths, timestamps, and the
// sk- boundary case -- must be left byte-identical.
func TestTextLeavesOrdinaryCodeAlone(t *testing.T) {
	ordinary := []string{
		`func BuildVectors(ctx context.Context, s *store.Store) (VectorPass, error) {`,
		`const releaseAt = "https://github.com/neur0map/prowl-agent/releases/download/"`,
		`hash := sha256.Sum256(body)`,
		`// Applies pattern matching to mask API keys, tokens, and credentials.`,
		`import "github.com/prowl-agent/prowl-agent/internal/store"`,
		`css := "#cdd6f4"`,
		`BudgetTokens: budgetTokens,`,
		`Author: proposal.Author,`,
		`MaxTokens:    rerankMaxTokens(len(candidates))`,
		`const tokensDocURL = "github.com/neur0map/prowl-agent/blob/main/docs/TOKENS.md"`,
		`sum := "d41d8cd98f00b204e9800998ecf8427e"`,
		`auth_note = "Use the token from the dashboard"`,
		`private_key_path = "C:\\Users\\me\\keys\\id_rsa"`,
		`token_expires_at = "2026-08-17T12:34:56.000Z"`,
		`disk-space-check-interval`,
	}
	for _, in := range ordinary {
		got, n := Text(in)
		if n != 0 {
			t.Fatalf("false positive on %q -> %q (masked %d)", in, got, n)
		}
		if got != in {
			t.Fatalf("output changed on %q -> %q", in, got)
		}
	}
}

// R13 recall tradeoff: a homegrown credential with no vendor prefix assigned to a
// secret-named variable is deliberately NOT masked. Asserted explicitly so the
// loss is visible and intentional, not an accident.
func TestTextDoesNotMaskUnprefixedSecret(t *testing.T) {
	in := `API_KEY = "8f3kd93kfj39dk20fjs93jf"`
	got, n := Text(in)
	if n != 0 {
		t.Fatalf("expected no masking under R13, masked %d -> %q", n, got)
	}
	if got != in {
		t.Fatalf("output changed: %q", got)
	}
}

// A complete PEM block: the body is destroyed but BOTH markers survive.
func TestTextMasksPairedPEM(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"
	got, n := Text(in)
	if n != 1 {
		t.Fatalf("masked %d values, want 1: %q", n, got)
	}
	if strings.Contains(got, "MIIEowIBAAKCAQEA1234") {
		t.Fatalf("key body survived: %q", got)
	}
	if !strings.Contains(got, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Fatalf("BEGIN marker lost: %q", got)
	}
	if !strings.Contains(got, "-----END RSA PRIVATE KEY-----") {
		t.Fatalf("END marker lost: %q", got)
	}
}

// P0 regression: a paired block preceded by source must not destroy that source.
// The single-alternation regex let Go's leftmost-match rule prefer a start-anchored
// tail alternative, wiping everything before the BEGIN marker.
func TestTextMasksPairedPEMPrecededBySource(t *testing.T) {
	in := "func loadKey() {\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----\n}"
	got, n := Text(in)
	if n != 1 {
		t.Fatalf("masked %d values, want 1: %q", n, got)
	}
	if !strings.HasPrefix(got, "func loadKey() {\n") {
		t.Fatalf("preceding source lost: %q", got)
	}
	if !strings.HasSuffix(got, "\n}") {
		t.Fatalf("trailing source lost: %q", got)
	}
	if strings.Contains(got, "MIIEowIBAAKCAQEA1234") {
		t.Fatalf("key body survived: %q", got)
	}
	if !strings.Contains(got, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Fatalf("BEGIN marker lost: %q", got)
	}
	if !strings.Contains(got, "-----END RSA PRIVATE KEY-----") {
		t.Fatalf("END marker lost: %q", got)
	}
}

// Head half of a key split across chunks: BEGIN marker with a body and no END.
func TestTextMasksUnpairedBeginPEM(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n"
	got, n := Text(in)
	if n != 1 {
		t.Fatalf("masked %d values, want 1: %q", n, got)
	}
	if strings.Contains(got, "MIIEowIBAAKCAQEA1234") {
		t.Fatalf("key body survived: %q", got)
	}
	if !strings.Contains(got, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Fatalf("BEGIN marker lost: %q", got)
	}
	if !strings.Contains(got, Mask) {
		t.Fatalf("no mask marker in %q", got)
	}
}

// Tail half of a split key: a body and an END marker with no BEGIN.
func TestTextMasksUnpairedEndPEM(t *testing.T) {
	in := "MIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"
	got, n := Text(in)
	if n != 1 {
		t.Fatalf("masked %d values, want 1: %q", n, got)
	}
	if strings.Contains(got, "MIIEowIBAAKCAQEA1234") {
		t.Fatalf("key body survived: %q", got)
	}
	if !strings.Contains(got, "-----END RSA PRIVATE KEY-----") {
		t.Fatalf("END marker lost: %q", got)
	}
	if !strings.Contains(got, Mask) {
		t.Fatalf("no mask marker in %q", got)
	}
}

// A second pass over already-masked text masks nothing and changes nothing.
func TestTextIdempotence(t *testing.T) {
	cases := []struct{ name, in string }{
		{"provider", `STRIPE_TOKEN = "sk_live_` + `51H8xQ2eZvKYlo2CabcdefghijklmnopQRST"`},
		{"url", `DATABASE_URL=postgres://user:secretPassword123456789@db.example.com:5432/db`},
		{"paired pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"},
		{"unpaired begin pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n"},
		{"unpaired end pem", "MIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			masked1, n1 := Text(c.in)
			if n1 != 1 {
				t.Fatalf("first pass masked %d, want 1: %q", n1, masked1)
			}
			masked2, n2 := Text(masked1)
			if n2 != 0 {
				t.Fatalf("second pass masked %d, want 0: %q", n2, masked2)
			}
			if masked1 != masked2 {
				t.Fatalf("not idempotent: %q != %q", masked1, masked2)
			}
		})
	}
}

// Inputs with no key material to redact must not inflate the count: a lone END
// marker with no body, and a paired block whose body is empty.
func TestTextNoCountInflation(t *testing.T) {
	cases := []struct{ name, in string }{
		{"lone end marker", "-----END RSA PRIVATE KEY-----"},
		{"empty body", "-----BEGIN RSA PRIVATE KEY-----" + "-----END RSA PRIVATE KEY-----"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := Text(c.in)
			if n != 0 {
				t.Fatalf("count inflated to %d on %q -> %q", n, c.in, got)
			}
			if got != c.in {
				t.Fatalf("output changed: %q", got)
			}
		})
	}
}
