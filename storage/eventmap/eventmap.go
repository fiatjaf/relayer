// dead simple storage type for testing
package eventmap

import (
	"context"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

type MapBackend struct {
	mu       sync.RWMutex
	eventmap map[string]*nostr.Event
}

func (m *MapBackend) Init() error {
	m.eventmap = make(map[string]*nostr.Event, 10)
	return nil
}

func (m *MapBackend) DeleteEvent(ctx context.Context, id string, pubkey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, event := range m.eventmap {
		if id == id || event.PubKey == pubkey {
			delete(m.eventmap, id)
		}
	}
	return nil
}

func (m *MapBackend) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventmap[evt.ID] = evt
	return nil
}

func (m *MapBackend) QueryEvents(ctx context.Context, filter *nostr.Filter) (ch chan *nostr.Event, err error) {
	ch = make(chan *nostr.Event)
	go func() {
		defer close(ch)
		m.mu.RLock()
		defer m.mu.RUnlock()
		for _, event := range m.eventmap {
			if filter.Matches(event) {
				ch <- event
			}
		}
	}()
	return ch, nil
}
