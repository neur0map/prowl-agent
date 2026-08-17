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

// Negative cases from real repo: identifiers and code that must NOT be masked.
func TestTextRejectsIdentifiersAndCommonCode(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"camelCase assignment", `BudgetTokens: budgetTokens,`},
		{"dotted selector", `Author: proposal.Author,`},
		{"URL constant", `const tokensDocURL = "github.com/neur0map/prowl-agent/blob/main/docs/TOKENS.md"`},
		{"function call assignment", `MaxTokens: rerankMaxTokens(len(candidates))`},
	}
	for _, c := range cases {
		if got, n := Text(c.in); n != 0 {
			t.Fatalf("false positive on %s: %q -> %q", c.name, c.in, got)
		}
	}
}

// Positive cases for assigned pattern: values that MUST be masked.
func TestTextMasksAssignedValues(t *testing.T) {
	cases := []struct {
		name, in string
	}{
		{"double-quoted secret", `API_KEY = "8f3kd93kfj39dk20fjs93jf1234567890"`},
		{"single-quoted secret", `API_KEY = '8f3kd93kfj39dk20fjs93jf1234567890'`},
	}
	for _, c := range cases {
		got, n := Text(c.in)
		if n != 1 {
			t.Fatalf("%s: masked %d values, want 1 (input: %q)", c.name, n, c.in)
		}
		if !strings.Contains(got, Mask) {
			t.Fatalf("%s: no mask marker in %q", c.name, got)
		}
		// Verify identifier survived
		if !strings.Contains(got, "API_KEY") {
			t.Fatalf("%s: identifier lost from %q", c.name, got)
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

// Test unpaired PEM marker (key split across chunks).
func TestTextMasksUnpairedPEM(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n"
	got, n := Text(input)
	if n != 1 {
		t.Fatalf("unpaired PEM: masked %d values, want 1", n)
	}
	if strings.Contains(got, "MIIEowIBAAKCAQEA") {
		t.Fatalf("unpaired PEM key leaked: %q", got)
	}
	if !strings.Contains(got, Mask) {
		t.Fatalf("unpaired PEM: no mask marker in %q", got)
	}
}
