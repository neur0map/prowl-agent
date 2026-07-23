package context

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestStoreTracerDoesNotPersistQuestionOrContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	secret := "SECRET-MARKER-never-persist"
	service := Service{Store: db, Tracer: StoreTracer{Store: db}}
	if _, err := service.Search(Request{Question: secret, Mode: ModeCompact, BudgetTokens: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(Request{}); err == nil {
		t.Fatal("expected validation error")
	}
	runs, err := db.ListContextRuns(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].QueryHash == "" || runs[0].HashVersion != traceHashVersion {
		t.Fatalf("runs = %+v", runs)
	}
	statuses := map[string]bool{}
	for _, run := range runs {
		statuses[run.Status] = true
		if bytes.Contains([]byte(run.QueryHash+run.SelectedIDsJSON+run.OmissionsJSON+run.TimingsJSON), []byte(secret)) {
			t.Fatalf("trace row leaked question: %+v", run)
		}
	}
	if !statuses["success"] || !statuses["error"] {
		t.Fatalf("trace statuses = %+v", statuses)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("raw sqlite database contains question text")
	}
}

type failingTracer struct{}

func (failingTracer) Record(TraceEvent) error { return errors.New("trace unavailable") }

func TestTraceFailureDoesNotFailRetrieval(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := Service{Store: db, Tracer: failingTracer{}}
	packet, err := service.Search(Request{Question: "nothing", Mode: ModeCompact, BudgetTokens: 20})
	if err != nil || packet.Items == nil {
		t.Fatalf("packet=%+v err=%v", packet, err)
	}
}
