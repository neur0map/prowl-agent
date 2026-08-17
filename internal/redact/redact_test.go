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
