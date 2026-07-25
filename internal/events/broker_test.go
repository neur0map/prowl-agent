package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConnectedSubscriberSweepRepairsFailedPublication(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 2, SnapshotURI: "snapshot://workspace-a/2"})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	broker, err := NewBroker(outbox, BrokerOptions{SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("Broker.Close() error = %v", err)
		}
	}()

	initial, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	subscription, err := broker.Subscribe(ctx, initial.Head)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("Subscription.Close() error = %v", err)
		}
	}()

	transaction, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transaction.Append("job.created", []byte("created")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	committed, err := transaction.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	forced := errors.New("forced publish failure")
	if err := broker.FailNextPublish(forced); err != nil {
		t.Fatalf("FailNextPublish() error = %v", err)
	}
	if err := broker.PublishCommitted(ctx); !errors.Is(err, forced) {
		t.Fatalf("PublishCommitted() error = %v, want forced failure", err)
	}
	assertNoDelivery(t, subscription)

	watermark, err := outbox.PublisherWatermark(ctx)
	if err != nil {
		t.Fatalf("PublisherWatermark() error = %v", err)
	}
	if watermark.Sequence != 0 {
		t.Fatalf("watermark after failed publication = %d, want 0", watermark.Sequence)
	}

	if err := broker.Sweep(ctx); err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	delivery, err := nextDelivery(t, subscription)
	if err != nil {
		t.Fatalf("Next() after Sweep() error = %v", err)
	}
	if delivery.Reset != nil || delivery.Event == nil {
		t.Fatalf("delivery after Sweep() = %#v, want event", delivery)
	}
	if delivery.Event.Cursor != committed[0].Cursor {
		t.Fatalf("repaired cursor = %#v, want %#v", delivery.Event.Cursor, committed[0].Cursor)
	}
	if got, want := string(delivery.Event.Payload), "created"; got != want {
		t.Fatalf("repaired payload = %q, want %q", got, want)
	}

	watermark, err = outbox.PublisherWatermark(ctx)
	if err != nil {
		t.Fatalf("PublisherWatermark() after Sweep() error = %v", err)
	}
	if watermark != committed[0].Cursor {
		t.Fatalf("watermark after Sweep() = %#v, want %#v", watermark, committed[0].Cursor)
	}
}

func TestAdapterConformanceReturnsValidationErrorsForMissingOutbox(t *testing.T) {
	var broker Broker
	ctx := context.Background()

	if _, err := broker.Subscribe(ctx, Cursor{}); !errors.Is(err, ErrNilOutbox) {
		t.Fatalf("Subscribe() error = %v, want ErrNilOutbox", err)
	}
	if err := broker.PublishCommitted(ctx); !errors.Is(err, ErrNilOutbox) {
		t.Fatalf("PublishCommitted() error = %v, want ErrNilOutbox", err)
	}
	if err := broker.Sweep(ctx); !errors.Is(err, ErrNilOutbox) {
		t.Fatalf("Sweep() error = %v, want ErrNilOutbox", err)
	}
}

func TestSlowSubscriberCloseIsIdempotentAndValidatesHandle(t *testing.T) {
	var zero Subscription
	if err := zero.Close(); !errors.Is(err, ErrNilSubscription) {
		t.Fatalf("zero Subscription.Close() error = %v, want ErrNilSubscription", err)
	}

	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	broker, err := NewBroker(outbox, BrokerOptions{SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("Broker.Close() error = %v", err)
		}
	}()
	state, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	subscription, err := broker.Subscribe(ctx, state.Head)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatalf("first Subscription.Close() error = %v", err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatalf("second Subscription.Close() error = %v", err)
	}
}

func TestSlowSubscriberQueueOverflowResetsAndUnrelatedStreamsDoNotLeak(t *testing.T) {
	ctx := context.Background()
	outboxA, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1, SnapshotURI: "snapshot://workspace-a/1"})
	if err != nil {
		t.Fatalf("NewMemoryOutbox(A) error = %v", err)
	}
	brokerA, err := NewBroker(outboxA, BrokerOptions{QueueCapacity: 1, SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker(A) error = %v", err)
	}
	defer func() {
		if err := brokerA.Close(); err != nil {
			t.Errorf("Broker.Close(A) error = %v", err)
		}
	}()

	initialA, err := outboxA.State(ctx)
	if err != nil {
		t.Fatalf("State(A) error = %v", err)
	}
	subscription, err := brokerA.Subscribe(ctx, initialA.Head)
	if err != nil {
		t.Fatalf("Subscribe(A) error = %v", err)
	}
	defer func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("Subscription.Close() error = %v", err)
		}
	}()

	committedA := appendAndCommit(t, outboxA, "job.created", []byte("first"))
	if err := brokerA.PublishCommitted(ctx); err != nil {
		t.Fatalf("PublishCommitted(first) error = %v", err)
	}
	_ = committedA
	committedA = appendAndCommit(t, outboxA, "job.updated", []byte("second"))
	if err := brokerA.PublishCommitted(ctx); err != nil {
		t.Fatalf("PublishCommitted(second) error = %v", err)
	}

	delivery, err := nextDelivery(t, subscription)
	if err != nil {
		t.Fatalf("Next() after overflow error = %v", err)
	}
	if delivery.Reset == nil || delivery.Event != nil {
		t.Fatalf("delivery after overflow = %#v, want reset", delivery)
	}
	if delivery.Reset.Cursor != committedA.Cursor {
		t.Fatalf("overflow reset cursor = %#v, want %#v", delivery.Reset.Cursor, committedA.Cursor)
	}
	if got, want := delivery.Reset.SnapshotURI, "snapshot://workspace-a/1"; got != want {
		t.Fatalf("overflow snapshot URI = %q, want %q", got, want)
	}
	assertNoDelivery(t, subscription)

	outboxB, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-b", Epoch: 1, SnapshotURI: "snapshot://workspace-b/1"})
	if err != nil {
		t.Fatalf("NewMemoryOutbox(B) error = %v", err)
	}
	brokerB, err := NewBroker(outboxB, BrokerOptions{SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker(B) error = %v", err)
	}
	defer func() {
		if err := brokerB.Close(); err != nil {
			t.Errorf("Broker.Close(B) error = %v", err)
		}
	}()
	initialB, err := outboxB.State(ctx)
	if err != nil {
		t.Fatalf("State(B) error = %v", err)
	}
	subscriptionB, err := brokerB.Subscribe(ctx, initialB.Head)
	if err != nil {
		t.Fatalf("Subscribe(B) error = %v", err)
	}
	defer func() {
		if err := subscriptionB.Close(); err != nil {
			t.Errorf("Subscription.Close(B) error = %v", err)
		}
	}()
	_ = appendAndCommit(t, outboxB, "job.created", []byte("other"))
	if err := brokerB.PublishCommitted(ctx); err != nil {
		t.Fatalf("PublishCommitted(B) error = %v", err)
	}
	if _, err := nextDelivery(t, subscriptionB); err != nil {
		t.Fatalf("Next(B) error = %v", err)
	}
	assertNoDelivery(t, subscription)
}

func TestSlowSubscriberLargeReplayResetsWithoutUnboundedDelivery(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1, SnapshotURI: "snapshot://workspace-a/1"})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	initial, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	transaction, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	payload := make([]byte, 128*1024)
	for index := range payload {
		payload[index] = byte(index)
	}
	for range 3 {
		if err := transaction.Append("job.updated", payload); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	committed, err := transaction.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	broker, err := NewBroker(outbox, BrokerOptions{QueueCapacity: 1, SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("Broker.Close() error = %v", err)
		}
	}()
	subscription, err := broker.Subscribe(ctx, initial.Head)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() {
		if err := subscription.Close(); err != nil {
			t.Errorf("Subscription.Close() error = %v", err)
		}
	}()

	delivery, err := nextDelivery(t, subscription)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if delivery.Reset == nil || delivery.Event != nil {
		t.Fatalf("large replay delivery = %#v, want reset", delivery)
	}
	if got, want := delivery.Reset.Cursor, committed[len(committed)-1].Cursor; got != want {
		t.Fatalf("large replay reset cursor = %#v, want %#v", got, want)
	}
	assertNoDelivery(t, subscription)
}

func TestSlowSubscriberCloseCancelsBlockingSweep(t *testing.T) {
	ctx := context.Background()
	base, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	outbox := &blockingOutbox{
		Outbox:  base,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	broker, err := NewBroker(outbox, BrokerOptions{SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	state, err := base.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if _, err := broker.Subscribe(ctx, state.Head); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	select {
	case <-outbox.started:
	case <-time.After(3 * time.Second):
		t.Fatal("periodic Sweep() did not reach blocking outbox I/O")
	}

	closed := make(chan error, 1)
	go func() {
		closed <- broker.Close()
	}()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Broker.Close() error = %v", err)
		}
	case <-time.After(time.Second):
		close(outbox.release)
		if err := <-closed; err != nil {
			t.Fatalf("Broker.Close() after release error = %v", err)
		}
		t.Fatal("Broker.Close() did not cancel blocking periodic sweep")
	}
}

func TestAdapterConformancePublishesBoundedBatches(t *testing.T) {
	ctx := context.Background()
	outbox, err := NewMemoryOutbox(MemoryOutboxConfig{ScopeID: "workspace-a", Epoch: 1})
	if err != nil {
		t.Fatalf("NewMemoryOutbox() error = %v", err)
	}
	broker, err := NewBroker(outbox, BrokerOptions{QueueCapacity: 4, PublishBatchSize: 1, SweepInterval: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewBroker() error = %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			t.Errorf("Broker.Close() error = %v", err)
		}
	}()
	state, err := outbox.State(ctx)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	subscription, err := broker.Subscribe(ctx, state.Head)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	transaction, err := outbox.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	for range 3 {
		if err := transaction.Append("job.updated", []byte("batch")); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
	committed, err := transaction.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := broker.PublishCommitted(ctx); err != nil {
		t.Fatalf("PublishCommitted() error = %v", err)
	}
	for index := range committed {
		delivery, err := nextDelivery(t, subscription)
		if err != nil {
			t.Fatalf("Next(%d) error = %v", index, err)
		}
		if delivery.Event == nil || delivery.Reset != nil {
			t.Fatalf("delivery(%d) = %#v, want event", index, delivery)
		}
		if got, want := delivery.Event.Cursor, committed[index].Cursor; got != want {
			t.Fatalf("delivery(%d) cursor = %#v, want %#v", index, got, want)
		}
	}
	watermark, err := outbox.PublisherWatermark(ctx)
	if err != nil {
		t.Fatalf("PublisherWatermark() error = %v", err)
	}
	if got, want := watermark, committed[len(committed)-1].Cursor; got != want {
		t.Fatalf("watermark = %#v, want %#v", got, want)
	}
}

type blockingOutbox struct {
	Outbox
	started chan struct{}
	release chan struct{}
}

func (outbox *blockingOutbox) PublisherWatermark(ctx context.Context) (Cursor, error) {
	select {
	case outbox.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return Cursor{}, ctx.Err()
	case <-outbox.release:
		return outbox.Outbox.PublisherWatermark(ctx)
	}
}

func appendAndCommit(t *testing.T, outbox *MemoryOutbox, kind string, payload []byte) Event {
	t.Helper()
	transaction, err := outbox.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transaction.Append(kind, payload); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	committed, err := transaction.Commit(context.Background())
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	return committed[0]
}

func nextDelivery(t *testing.T, subscription *Subscription) (Delivery, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return subscription.Next(ctx)
}

func assertNoDelivery(t *testing.T, subscription *Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if delivery, err := subscription.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Next() while no delivery is queued = (%#v, %v), want deadline exceeded", delivery, err)
	}
}
