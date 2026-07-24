package apphost

import "sync"

// Event is a single fan-out message published by the core Service. It mirrors
// the (name, payload) shape of the desktop EventSink so subscribers can forward
// it verbatim to a Wails WebView, an SSE stream, or a test observer.
type Event struct {
	Name    string
	Payload any
}

// Subscription is one consumer's view onto the Hub. Read events from Events()
// until the channel closes; call Close() (or Hub.Close()) to detach.
type Subscription struct {
	id  uint64
	ch  chan Event
	hub *Hub
}

// Events returns the receive-only channel of fan-out events for this
// subscriber. The channel is closed when the subscription (or the hub) closes.
func (s *Subscription) Events() <-chan Event { return s.ch }

// Close detaches this subscriber from the hub and closes its channel. Safe to
// call multiple times.
func (s *Subscription) Close() {
	if s == nil || s.hub == nil {
		return
	}
	s.hub.unsubscribe(s.id)
}

// Hub is a mutex-guarded fan-out registry: the single core EventSink publishes
// once and every current subscriber receives a copy. Each subscriber has its
// own buffered channel; a slow consumer that has filled its buffer has the
// event dropped rather than blocking the publisher or its peers. This mirrors
// the existing watcher backpressure policy in the application service.
type Hub struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]chan Event
	closed bool
}

// NewHub returns an empty, ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[uint64]chan Event)}
}

const defaultSubscriberBuffer = 64

// Subscribe registers a new consumer. buffer is the per-subscriber channel
// depth; values <= 0 use a sensible default. Subscribing on a closed hub
// returns a subscription whose channel is already closed.
func (h *Hub) Subscribe(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = defaultSubscriberBuffer
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan Event, buffer)
	if h.closed {
		close(ch)
		return &Subscription{ch: ch, hub: h}
	}
	id := h.nextID
	h.nextID++
	h.subs[id] = ch
	return &Subscription{id: id, ch: ch, hub: h}
}

func (h *Hub) unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
}

// Publish delivers name/payload to every current subscriber. It is the
// application.EventSink the core Service is constructed with. Delivery is
// non-blocking: a subscriber whose buffer is full misses this event.
func (h *Hub) Publish(name string, payload any) {
	ev := Event{Name: name, Payload: payload}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// Slow consumer: drop rather than stall the publisher.
		}
	}
}

// SubscriberCount reports the number of attached subscribers (test/observability aid).
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Close detaches all subscribers and prevents new ones from receiving events.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for id, ch := range h.subs {
		delete(h.subs, id)
		close(ch)
	}
	h.closed = true
}
