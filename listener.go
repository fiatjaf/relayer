package relayer

import (
	"github.com/nbd-wtf/go-nostr"
)

type Listener struct {
	filters nostr.Filters
}

func (rl *Listeners) GetListeningFilters() nostr.Filters {
	respfilters := make(nostr.Filters, 0, len(rl.listeners)*2)

	rl.listenersMutex.Lock()
	defer rl.listenersMutex.Unlock()

	// here we go through all the existing listeners
	for _, connlisteners := range rl.listeners {
		for _, listener := range connlisteners {
			for _, listenerfilter := range listener.filters {
				for _, respfilter := range respfilters {
					// check if this filter specifically is already added to respfilters
					if nostr.FilterEqual(listenerfilter, respfilter) {
						goto nextconn
					}
				}

				// field not yet present on respfilters, add it
				respfilters = append(respfilters, listenerfilter)

				// continue to the next filter
			nextconn:
				continue
			}
		}
	}

	// respfilters will be a slice with all the distinct filter we currently have active
	return respfilters
}

func setListener(relay Relay, id string, ws *WebSocket, filters nostr.Filters) {
	if b, ok := relay.(RelayListener); ok {
		if rl := b.Listeners(); rl != nil {
			rl.setListener(id, ws, filters)
		}
	}
}

func (rl *Listeners) setListener(id string, ws *WebSocket, filters nostr.Filters) {
	rl.listenersMutex.Lock()
	defer rl.listenersMutex.Unlock()

	subs, ok := rl.listeners[ws]
	if !ok {
		subs = make(map[string]*Listener)
		rl.listeners[ws] = subs
	}

	subs[id] = &Listener{filters: filters}
}

func removeListenerId(relay Relay, ws *WebSocket, id string) {
	if b, ok := relay.(RelayListener); ok {
		if rl := b.Listeners(); rl != nil {
			rl.removeListenerId(ws, id)
		}
	}
}

// Remove a specific subscription id from listeners for a given ws client
func (rl *Listeners) removeListenerId(ws *WebSocket, id string) {
	rl.listenersMutex.Lock()
	defer rl.listenersMutex.Unlock()

	if subs, ok := rl.listeners[ws]; ok {
		delete(rl.listeners[ws], id)
		if len(subs) == 0 {
			delete(rl.listeners, ws)
		}
	}
}

func removeListener(relay Relay, ws *WebSocket) {
	if b, ok := relay.(RelayListener); ok {
		if rl := b.Listeners(); rl != nil {
			rl.removeListener(ws)
		}
	}
}

// Remove WebSocket conn from listeners
func (rl *Listeners) removeListener(ws *WebSocket) {
	rl.listenersMutex.Lock()
	defer rl.listenersMutex.Unlock()
	clear(rl.listeners[ws])
	delete(rl.listeners, ws)
}

func notifyListeners(relay Relay, event *nostr.Event) {
	if b, ok := relay.(RelayListener); ok {
		if rl := b.Listeners(); rl != nil {
			rl.notifyListeners(event)
		}
	}
}

func (rl *Listeners) notifyListeners(event *nostr.Event) {
	rl.listenersMutex.Lock()
	defer rl.listenersMutex.Unlock()

	for ws, subs := range rl.listeners {
		for id, listener := range subs {
			if !listener.filters.Match(event) {
				continue
			}
			ws.WriteJSON(nostr.EventEnvelope{SubscriptionID: &id, Event: *event})
		}
	}
}
