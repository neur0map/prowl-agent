package workbench

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/events"
	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

func TestSSEEmitsRedactedReplayAndUnsubscribesOnDisconnect(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := jobs.Open(context.Background(), project.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := events.NewBroker(events.NewProjectJobsOutbox(store), events.BrokerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	jobService := jobs.NewService(store, broker, nil)
	if err := project.AttachJobsService(jobService); err != nil {
		t.Fatal(err)
	}
	if err := service.AttachLiveOperations(jobService, broker); err != nil {
		t.Fatal(err)
	}
	job, _, err := jobService.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := jobService.StreamState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events?stream_scope=project-job&scope_id="+state.ScopeID+"&epoch="+itoa(state.Epoch)+"&sequence=0", nil).WithContext(ctx)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	response := &sseRecorder{ResponseRecorder: httptest.NewRecorder(), wrote: make(chan struct{})}
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-response.wrote:
	case <-time.After(time.Second):
		t.Fatal("stream did not emit replay")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after request cancellation")
	}
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"sequence":1`) || strings.Contains(body, project.Workspace.Root) || strings.Contains(body, job.ID) {
		t.Fatalf("unredacted or missing delivery: %q", body)
	}
}

type sseRecorder struct {
	*httptest.ResponseRecorder
	wrote chan struct{}
	once  sync.Once
}

func (recorder *sseRecorder) Write(body []byte) (int, error) {
	written, err := recorder.ResponseRecorder.Write(body)
	if strings.Contains(string(body), "event: ") {
		recorder.once.Do(func() { close(recorder.wrote) })
	}
	return written, err
}

func (recorder *sseRecorder) Flush() {}

func TestSSEResetPreservesSafeC1SnapshotURI(t *testing.T) {
	response := httptest.NewRecorder()
	delivery := events.Delivery{Reset: &events.Reset{Cursor: events.Cursor{StreamScope: events.ProjectJob, ScopeID: "scope", Epoch: 1, Sequence: 2}, SnapshotURI: "snapshot://retained"}}
	if err := writeSSEDelivery(response, delivery); err != nil {
		t.Fatal(err)
	}
	if body := response.Body.String(); !strings.Contains(body, `"snapshot_uri":"snapshot://retained"`) || strings.Contains(body, "/api/v1/jobs/snapshot") {
		t.Fatalf("reset body=%q", body)
	}
}
func itoa(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
