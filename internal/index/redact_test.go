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
// retrieval path, so masking has to happen before storage. The decisive check
// reads the stored chunk text directly (FirstChunk), not a search result: if
// masking were reimplemented as a filter inside SearchChunks, the raw secret
// would still sit in chunks.text and this assertion would fail. SearchChunks is
// used only to confirm the file is still findable after masking.
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

	// Direct at-rest read of the stored chunk text.
	chunk, ok, err := s.FirstChunk("config.py")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("config.py has no stored chunk")
	}
	if strings.Contains(chunk.Text, "sk_live_"+"51H8xQ2eZvKYlo2C") {
		t.Fatalf("secret stored in cleartext in chunks.text: %q", chunk.Text)
	}
	if !strings.Contains(chunk.Text, redact.Mask) {
		t.Fatalf("mask marker absent from stored chunk; masking did not run at index time: %q", chunk.Text)
	}
	if !strings.Contains(chunk.Text, "STRIPE_TOKEN") {
		t.Fatalf("identifier destroyed along with the value: %q", chunk.Text)
	}
}
