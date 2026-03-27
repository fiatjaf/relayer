package relayer

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func listenerCount(ws *WebSocket) int {
	listenersMutex.Lock()
	defer listenersMutex.Unlock()
	return len(listeners[ws])
}

func totalConnections() int {
	listenersMutex.Lock()
	defer listenersMutex.Unlock()
	return len(listeners)
}

func hasListener(ws *WebSocket, id string) bool {
	listenersMutex.Lock()
	defer listenersMutex.Unlock()
	if subs, ok := listeners[ws]; ok {
		_, ok = subs[id]
		return ok
	}
	return false
}

func TestSetListener(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})

	if !hasListener(ws, "sub1") {
		t.Error("sub1 not found")
	}
	if listenerCount(ws) != 1 {
		t.Errorf("expected 1 sub, got %d", listenerCount(ws))
	}
}

func TestSetListenerMultipleSubs(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	if listenerCount(ws) != 2 {
		t.Errorf("expected 2 subs, got %d", listenerCount(ws))
	}
}

func TestSetListenerOverwrite(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{2}}})

	if listenerCount(ws) != 1 {
		t.Errorf("expected 1 sub after overwrite, got %d", listenerCount(ws))
	}
}

func TestSetListenerMultipleWS(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws1 := &WebSocket{}
	ws2 := &WebSocket{}
	setListener("sub1", ws1, nostr.Filters{{Kinds: []int{1}}})
	setListener("sub1", ws2, nostr.Filters{{Kinds: []int{1}}})

	if totalConnections() != 2 {
		t.Errorf("expected 2 connections, got %d", totalConnections())
	}
}

func TestRemoveListenerId(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	removeListenerId(ws, "sub1")

	if hasListener(ws, "sub1") {
		t.Error("sub1 should be removed")
	}
	if !hasListener(ws, "sub2") {
		t.Error("sub2 should still exist")
	}
}

func TestRemoveListenerIdRemovesWSWhenEmpty(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})

	removeListenerId(ws, "sub1")

	if totalConnections() != 0 {
		t.Error("ws entry should be removed when all subs are gone")
	}
}

func TestRemoveListenerIdNonexistent(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	// should not panic
	removeListenerId(ws, "nope")
}

func TestRemoveListener(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	setListener("sub1", ws, nostr.Filters{{Kinds: []int{1}}})
	setListener("sub2", ws, nostr.Filters{{Kinds: []int{2}}})

	removeListener(ws)

	if totalConnections() != 0 {
		t.Error("ws should be removed")
	}
}

func TestRemoveListenerNonexistent(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws := &WebSocket{}
	// should not panic
	removeListener(ws)
}

func TestGetListeningFilters(t *testing.T) {
	clearListeners()
	defer clearListeners()

	ws1 := &WebSocket{}
	ws2 := &WebSocket{}

	f1 := nostr.Filter{Kinds: []int{1}}
	f2 := nostr.Filter{Kinds: []int{2}}

	setListener("sub1", ws1, nostr.Filters{f1, f2})
	setListener("sub2", ws2, nostr.Filters{f1}) // duplicate of f1

	filters := GetListeningFilters()
	if len(filters) != 2 {
		t.Errorf("expected 2 distinct filters, got %d", len(filters))
	}
}

func TestGetListeningFiltersEmpty(t *testing.T) {
	clearListeners()
	defer clearListeners()

	filters := GetListeningFilters()
	if len(filters) != 0 {
		t.Errorf("expected 0 filters, got %d", len(filters))
	}
}
