package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultQueueCapacity    = 64
	DefaultPublishBatchSize = 64
	MinimumSweepInterval    = 2 * time.Second
)

var (
	ErrNilBroker             = errors.New("nil broker")
	ErrNilSubscription       = errors.New("nil subscription")
	ErrBrokerClosed          = errors.New("event broker is closed")
	ErrSubscriptionClosed    = errors.New("event subscription is closed")
	ErrInvalidBrokerOption   = errors.New("invalid broker option")
	ErrInvalidPublishFailure = errors.New("invalid publish failure")
	ErrPublishFailurePending = errors.New("a publish failure is already pending")
	ErrInvalidReplay         = errors.New("invalid outbox replay")
)

type BrokerOptions struct {
	QueueCapacity    int
	PublishBatchSize int
	SweepInterval    time.Duration
}

type Delivery struct {
	Event *Event
	Reset *Reset
}

type Broker struct {
	outbox Outbox

	publishMu sync.Mutex
	mu        sync.Mutex
	options   BrokerOptions
	subs      map[*Subscription]struct{}
	closed    bool
	failNext  error

	sweepCancel context.CancelFunc
	sweepDone   chan struct{}
}

type Subscription struct {
	broker *Broker

	mu       sync.Mutex
	capacity int
	queue    []Delivery
	last     Cursor
	closed   bool
	notify   chan struct{}
}

func NewBroker(outbox Outbox, options BrokerOptions) (*Broker, error) {
	if outbox == nil {
		return nil, ErrNilOutbox
	}
	if options.QueueCapacity == 0 {
		options.QueueCapacity = DefaultQueueCapacity
	}
	if options.QueueCapacity < 1 {
		return nil, fmt.Errorf("%w: queue capacity must be positive", ErrInvalidBrokerOption)
	}
	if options.PublishBatchSize == 0 {
		options.PublishBatchSize = DefaultPublishBatchSize
	}
	if options.PublishBatchSize < 1 {
		return nil, fmt.Errorf("%w: publish batch size must be positive", ErrInvalidBrokerOption)
	}
	if options.SweepInterval == 0 {
		options.SweepInterval = MinimumSweepInterval
	}
	if options.SweepInterval < MinimumSweepInterval {
		return nil, fmt.Errorf("%w: sweep interval must be at least %s", ErrInvalidBrokerOption, MinimumSweepInterval)
	}
	return &Broker{
		outbox:  outbox,
		options: options,
		subs:    make(map[*Subscription]struct{}),
	}, nil
}

func (broker *Broker) Subscribe(ctx context.Context, after Cursor) (*Subscription, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if broker == nil {
		return nil, ErrNilBroker
	}
	if broker.outbox == nil {
		return nil, ErrNilOutbox
	}

	broker.publishMu.Lock()
	defer broker.publishMu.Unlock()
	if broker.isClosed() {
		return nil, ErrBrokerClosed
	}

	replay, err := broker.outbox.Replay(ctx, after, broker.options.QueueCapacity)
	if err != nil {
		return nil, err
	}

	subscription := &Subscription{
		broker:   broker,
		capacity: broker.options.QueueCapacity,
		notify:   make(chan struct{}, 1),
	}
	if replay.Reset != nil {
		subscription.last = replay.Reset.Cursor
		subscription.queue = []Delivery{{Reset: cloneReset(replay.Reset)}}
	} else {
		subscription.last = after
		if replay.More || len(replay.Events) > subscription.capacity {
			state, err := broker.outbox.State(ctx)
			if err != nil {
				return nil, err
			}
			subscription.last = state.Head
			subscription.queue = []Delivery{{Reset: &Reset{Cursor: state.Head, SnapshotURI: state.SnapshotURI}}}
		} else {
			for _, event := range replay.Events {
				if err := validateReplayEvent(subscription.last, event); err != nil {
					return nil, err
				}
				subscription.queue = append(subscription.queue, Delivery{Event: eventPointer(event)})
				subscription.last = event.Cursor
			}
		}
	}

	broker.mu.Lock()
	if broker.closed {
		broker.mu.Unlock()
		return nil, ErrBrokerClosed
	}
	broker.subs[subscription] = struct{}{}
	broker.startSweepLocked()
	broker.mu.Unlock()
	return subscription, nil
}

func (broker *Broker) PublishCommitted(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if broker == nil {
		return ErrNilBroker
	}
	if broker.outbox == nil {
		return ErrNilOutbox
	}
	broker.publishMu.Lock()
	defer broker.publishMu.Unlock()
	if broker.isClosed() {
		return ErrBrokerClosed
	}
	return broker.publishLocked(ctx)
}

func (broker *Broker) Sweep(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if broker == nil {
		return ErrNilBroker
	}
	if broker.outbox == nil {
		return ErrNilOutbox
	}
	broker.publishMu.Lock()
	defer broker.publishMu.Unlock()
	if broker.isClosed() {
		return ErrBrokerClosed
	}
	if !broker.hasSubscribers() {
		return nil
	}
	return broker.publishLocked(ctx)
}

func (broker *Broker) FailNextPublish(err error) error {
	if err == nil {
		return ErrInvalidPublishFailure
	}
	if broker == nil {
		return ErrNilBroker
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if broker.closed {
		return ErrBrokerClosed
	}
	if broker.failNext != nil {
		return ErrPublishFailurePending
	}
	broker.failNext = err
	return nil
}

func (broker *Broker) Close() error {
	if broker == nil {
		return ErrNilBroker
	}

	broker.mu.Lock()
	if broker.closed {
		broker.mu.Unlock()
		return nil
	}
	broker.closed = true
	for subscription := range broker.subs {
		subscription.closeWithoutUnsubscribe()
	}
	broker.subs = make(map[*Subscription]struct{})
	cancel, done := broker.stopSweepLocked()
	broker.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	broker.publishMu.Lock()
	broker.publishMu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}

func (broker *Broker) publishLocked(ctx context.Context) error {
	watermark, err := broker.outbox.PublisherWatermark(ctx)
	if err != nil {
		return err
	}
	for {
		replay, err := broker.outbox.Replay(ctx, watermark, broker.options.PublishBatchSize)
		if err != nil {
			return err
		}
		if replay.Reset != nil {
			if replay.More {
				return fmt.Errorf("%w: reset replay cannot have more events", ErrInvalidReplay)
			}
			broker.broadcastReset(replay.Reset)
			return broker.outbox.SetPublisherWatermark(ctx, replay.Reset.Cursor)
		}

		last := watermark
		for _, event := range replay.Events {
			if err := validateReplayEvent(last, event); err != nil {
				return err
			}
			if failure := broker.takePublishFailure(); failure != nil {
				return failure
			}
			if err := broker.broadcastEvent(ctx, event); err != nil {
				return err
			}
			if err := broker.outbox.SetPublisherWatermark(ctx, event.Cursor); err != nil {
				return err
			}
			last = event.Cursor
		}
		if !replay.More {
			return nil
		}
		if len(replay.Events) == 0 {
			return fmt.Errorf("%w: truncated replay has no events", ErrInvalidReplay)
		}
		watermark = last
	}
}

func (broker *Broker) broadcastEvent(ctx context.Context, event Event) error {
	subscriptions := broker.subscriptionSnapshot()
	overflowed := make([]*Subscription, 0)
	for _, subscription := range subscriptions {
		if subscription.enqueueEvent(event) {
			overflowed = append(overflowed, subscription)
		}
	}
	if len(overflowed) == 0 {
		return nil
	}
	state, err := broker.outbox.State(ctx)
	if err != nil {
		return err
	}
	reset := Reset{Cursor: state.Head, SnapshotURI: state.SnapshotURI}
	for _, subscription := range overflowed {
		subscription.enqueueReset(reset)
	}
	return nil
}

func (broker *Broker) broadcastReset(reset *Reset) {
	if reset == nil {
		return
	}
	for _, subscription := range broker.subscriptionSnapshot() {
		subscription.enqueueReset(*reset)
	}
}

func (broker *Broker) subscriptionSnapshot() []*Subscription {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	subscriptions := make([]*Subscription, 0, len(broker.subs))
	for subscription := range broker.subs {
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions
}

func (broker *Broker) isClosed() bool {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return broker.closed
}

func (broker *Broker) hasSubscribers() bool {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return !broker.closed && len(broker.subs) > 0
}

func (broker *Broker) takePublishFailure() error {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	failure := broker.failNext
	broker.failNext = nil
	return failure
}

func (broker *Broker) startSweepLocked() {
	if broker.sweepCancel != nil || len(broker.subs) == 0 || broker.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	broker.sweepCancel = cancel
	broker.sweepDone = done
	go broker.sweepLoop(ctx, done)
}

func (broker *Broker) stopSweepLocked() (context.CancelFunc, chan struct{}) {
	cancel := broker.sweepCancel
	done := broker.sweepDone
	broker.sweepCancel = nil
	broker.sweepDone = nil
	return cancel, done
}

func (broker *Broker) sweepLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(broker.options.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = broker.Sweep(ctx)
		}
	}
}

func (broker *Broker) unsubscribe(subscription *Subscription) {
	broker.mu.Lock()
	if _, found := broker.subs[subscription]; !found {
		broker.mu.Unlock()
		return
	}
	delete(broker.subs, subscription)
	var cancel context.CancelFunc
	var done chan struct{}
	if len(broker.subs) == 0 {
		cancel, done = broker.stopSweepLocked()
	}
	broker.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (subscription *Subscription) Next(ctx context.Context) (Delivery, error) {
	if err := contextError(ctx); err != nil {
		return Delivery{}, err
	}
	if subscription == nil || subscription.broker == nil {
		return Delivery{}, ErrNilSubscription
	}
	for {
		subscription.mu.Lock()
		if subscription.closed {
			subscription.mu.Unlock()
			return Delivery{}, ErrSubscriptionClosed
		}
		if len(subscription.queue) > 0 {
			delivery := cloneDelivery(subscription.queue[0])
			subscription.queue = subscription.queue[1:]
			subscription.mu.Unlock()
			return delivery, nil
		}
		notify := subscription.notify
		subscription.mu.Unlock()

		select {
		case <-ctx.Done():
			return Delivery{}, ctx.Err()
		case <-notify:
		}
	}
}

func (subscription *Subscription) Close() error {
	if subscription == nil || subscription.broker == nil {
		return ErrNilSubscription
	}
	if !subscription.closeWithoutUnsubscribe() {
		return nil
	}
	subscription.broker.unsubscribe(subscription)
	return nil
}

func (subscription *Subscription) enqueueEvent(event Event) bool {
	subscription.mu.Lock()
	defer subscription.mu.Unlock()
	if subscription.closed || !sameStream(subscription.last, event.Cursor) || event.Cursor.Sequence <= subscription.last.Sequence {
		return false
	}
	if len(subscription.queue) >= subscription.capacity {
		return true
	}
	subscription.queue = append(subscription.queue, Delivery{Event: eventPointer(event)})
	subscription.last = event.Cursor
	subscription.signalLocked()
	return false
}

func (subscription *Subscription) enqueueReset(reset Reset) {
	subscription.mu.Lock()
	defer subscription.mu.Unlock()
	if subscription.closed {
		return
	}
	subscription.queue = []Delivery{{Reset: cloneReset(&reset)}}
	subscription.last = reset.Cursor
	subscription.signalLocked()
}

func (subscription *Subscription) closeWithoutUnsubscribe() bool {
	subscription.mu.Lock()
	defer subscription.mu.Unlock()
	if subscription.closed {
		return false
	}
	subscription.closed = true
	subscription.queue = nil
	subscription.signalLocked()
	return true
}

func (subscription *Subscription) signalLocked() {
	select {
	case subscription.notify <- struct{}{}:
	default:
	}
}

func validateReplayEvent(previous Cursor, event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if !sameStream(previous, event.Cursor) || event.Cursor.Sequence <= previous.Sequence {
		return fmt.Errorf("%w: event cursor %v follows %v", ErrInvalidReplay, event.Cursor, previous)
	}
	return nil
}

func cloneDelivery(delivery Delivery) Delivery {
	if delivery.Event != nil {
		event := cloneEvent(*delivery.Event)
		delivery.Event = &event
	}
	delivery.Reset = cloneReset(delivery.Reset)
	return delivery
}

func eventPointer(event Event) *Event {
	copy := cloneEvent(event)
	return &copy
}

func cloneReset(reset *Reset) *Reset {
	if reset == nil {
		return nil
	}
	copy := *reset
	return &copy
}
