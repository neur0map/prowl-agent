package redact

import "strings"
import "testing"

func TestTextMasksProviderCredentials(t *testing.T) {
	cases := []struct{ name, in, leak string }{
		{"stripe", `STRIPE_TOKEN = "sk_live_` + `51H8xQ2eZvKYlo2CabcdefghijklmnopQRST"`, "sk_live_" + "51H8xQ2eZvKYlo2C"},
		{"openai", `key = "sk-proj-` + `1234567890abcdefGHIJKLMNOPqrstuvwxyz0987654321"`, "sk-proj-" + "1234567890abcdef"},
		{"github", `token: ghp_` + `1234567890abcdefghijklmnopqrstuvwx`, "ghp_" + "1234567890abcdef"},
		{"aws id", `AWS_ACCESS_KEY_ID=AKIA` + `IOSFODNN7EXAMPLE`, "AKIA" + "IOSFODNN7EXAMPLE"},
		{"url creds", `DATABASE_URL=postgres://admin:sup3rs3cr3tpw@db.internal:5432/prod`, "sup3rs3cr3tpw"},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----", "MIIEowIBAAKCAQEA1234"},
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
// lines an agent is looking for.
func TestTextLeavesOrdinaryCodeAlone(t *testing.T) {
	ordinary := []string{
		`func BuildVectors(ctx context.Context, s *store.Store) (VectorPass, error) {`,
		`const releaseAt = "https://github.com/neur0map/prowl-agent/releases/download/"`,
		`hash := sha256.Sum256(body)`,
		`// Applies pattern matching to mask API keys, tokens, and credentials.`,
		`import "github.com/prowl-agent/prowl-agent/internal/store"`,
		`sum := "d41d8cd98f00b204e9800998ecf8427e"`,
		`css := "#cdd6f4"`,
	}
	for _, in := range ordinary {
		if got, n := Text(in); n != 0 {
			t.Fatalf("false positive on %q -> %q", in, got)
		}
	}
}

// Canonical test cases from round-2 ruling: assigned pattern must mask high-entropy
// secrets but reject plain code identifiers and URLs.
func TestTextCanonicalMaskCases(t *testing.T) {
	maskCases := []struct {
		name, in string
	}{
		{"high digit density", `API_KEY = "8f3kd93kfj39dk20fjs93jf"`},
		{"aws key with slash", `AWS_SECRET_ACCESS_KEY = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`},
		{"md5-like", `password = "d41d8cd98f00b204e9800998ecf8427ef"`},
		{"base64 token", `TOKEN = "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MA"`},
		{"mixed entropy", `api_secret = "kJ8fQ2mZ7xR4tY6uI0oP3aS5dF9gH1jK"`},
	}
	for _, c := range maskCases {
		got, n := Text(c.in)
		if n != 1 {
			t.Fatalf("%s: masked %d values, want 1", c.name, n)
		}
		if !strings.Contains(got, Mask) {
			t.Fatalf("%s: no mask marker in %q", c.name, got)
		}
	}
}

// Canonical test cases: plain code identifiers must NOT be masked.
func TestTextCanonicalUntouchedCases(t *testing.T) {
	untouched := []string{
		`BudgetTokens: budgetTokens,`,
		`Author: proposal.Author,`,
		`PacketTokens: pkt.Budget.EstimatedTokens`,
		`MaxTokens:    rerankMaxTokens(len(candidates))`,
		`const tokensDocURL = "github.com/neur0map/prowl-agent/blob/main/docs/TOKENS.md"`,
		`sum := "d41d8cd98f00b204e9800998ecf8427e"`,
	}
	for _, in := range untouched {
		if got, n := Text(in); n != 0 {
			t.Fatalf("false positive on %q -> %q (masked %d)", in, got, n)
		}
	}
}

// Test idempotence: masking an already-masked string should not increase count.
func TestTextIdempotence(t *testing.T) {
	input := `DATABASE_URL=postgres://user:secretPassword123456789@db.example.com:5432/db`
	masked1, n1 := Text(input)
	if n1 != 1 {
		t.Fatalf("expected 1 value masked, got %d", n1)
	}
	masked2, n2 := Text(masked1)
	if n2 != 0 {
		t.Fatalf("re-masking already-masked text counted %d new values, want 0", n2)
	}
	if masked1 != masked2 {
		t.Fatalf("idempotence failed: %q != %q", masked1, masked2)
	}
}

// Test unpaired PEM markers (key split across chunks or tail-only).
func TestTextMasksUnpairedPEM(t *testing.T) {
	cases := []struct {
		name, input string
	}{
		{"unpaired begin", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n"},
		{"unpaired end", "MIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----"},
	}
	for _, c := range cases {
		got, n := Text(c.input)
		if n != 1 {
			t.Fatalf("%s: masked %d values, want 1", c.name, n)
		}
		if !strings.Contains(got, Mask) {
			t.Fatalf("%s: no mask marker in %q", c.name, got)
		}
	}
}

// Test PEM idempotence: re-masking should not recount.
func TestTextPEMIdempotence(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----"
	masked1, n1 := Text(input)
	if n1 != 1 {
		t.Fatalf("expected 1 PEM masked, got %d", n1)
	}
	_, n2 := Text(masked1)
	if n2 != 0 {
		t.Fatalf("re-masking already-masked PEM counted %d new values, want 0", n2)
	}
}
