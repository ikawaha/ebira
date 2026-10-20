package service

import (
	"context"
	"sync"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// subscriberBuffer is the number of events buffered per subscriber. A
// subscriber that falls further behind is disconnected; clients reload the note
// list after reconnecting, so no change is lost.
const subscriberBuffer = 64

// broker fans out note events to subscribers.
type broker struct {
	mu     sync.Mutex
	subs   map[chan *gennotes.NoteEvent]struct{}
	closed bool
}

func newBroker() *broker {
	return &broker{subs: map[chan *gennotes.NoteEvent]struct{}{}}
}

// subscribe registers a subscriber. The returned channel is closed when the
// subscriber is too slow, when the broker is closed, or when cancel is called.
func (b *broker) subscribe() (ch <-chan *gennotes.NoteEvent, cancel func()) {
	c := make(chan *gennotes.NoteEvent, subscriberBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(c)
		return c, func() {}
	}
	b.subs[c] = struct{}{}
	return c, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[c]; ok {
			delete(b.subs, c)
			close(c)
		}
	}
}

func (b *broker) publish(typ string, ids ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		ev := &gennotes.NoteEvent{Type: typ, ID: &id}
		for c := range b.subs {
			select {
			case c <- ev:
			default:
				delete(b.subs, c)
				close(c)
			}
		}
	}
}

// publishAll sends an event that is not about a single note.
func (b *broker) publishAll(typ string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ev := &gennotes.NoteEvent{Type: typ}
	for c := range b.subs {
		select {
		case c <- ev:
		default:
			delete(b.subs, c)
			close(c)
		}
	}
}

// close disconnects all subscribers and rejects new ones.
func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for c := range b.subs {
		delete(b.subs, c)
		close(c)
	}
}

func (s *Notes) Events(ctx context.Context, _ *gennotes.EventsPayload, stream gennotes.EventsServerStream) error {
	ch, cancel := s.events.subscribe()
	defer cancel()
	// The response headers are written with the first event, so send one
	// right away to let the client know the stream is open.
	if err := stream.SendWithContext(ctx, &gennotes.NoteEvent{Type: eventReady}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.SendWithContext(ctx, ev); err != nil {
				return err
			}
		}
	}
}
