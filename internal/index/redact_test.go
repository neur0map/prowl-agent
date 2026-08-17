package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/redact"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// A credential committed in source must not reach the index. Output filtering is
// not enough: chunks.text and the FTS index are on disk and are read by every
// retrieval path, so masking has to happen before storage. The store exposes no
// raw DB handle, so at-rest masking is verified through its existing public
// reads: no result may carry the secret, and at least one must carry the mask --
// the mask is what proves the value was masked rather than the file simply
// missing from the index.
func TestIndexNeverStoresSecrets(t *testing.T) {
	root := t.TempDir()
	src := "# Load service credentials for billing.\n" +
		"STRIPE_TOKEN = \"sk_live_" + "51H8xQ2eZvKYlo2CabcdefghijklmnopQRST\"\n\n" +
		"def load_credentials():\n    return STRIPE_TOKEN\n"
	if err := os.WriteFile(filepath.Join(root, "config.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := Index(s, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Redacted == 0 {
		t.Fatal("summary reported no redactions")
	}

	rows, err := s.SearchChunks("credentials", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("precondition: the file must still be findable after masking")
	}
	var sawMask bool
	for _, r := range rows {
		if strings.Contains(r.Snippet, "sk_live_"+"51H8xQ2eZvKYlo2C") {
			t.Fatalf("secret leaked through search: %q", r.Snippet)
		}
		if strings.Contains(r.Snippet, redact.Mask) {
			sawMask = true
		}
	}
	if !sawMask {
		t.Fatalf("no result carried the mask %q; masking did not run at index time: %+v", redact.Mask, rows)
	}
}
