package relayer

import (
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

type Listener struct {
	filters nostr.Filters
}

var listeners = make(map[*WebSocket]map[string]*Listener)
var listenersMutex = sync.Mutex{}

func GetListeningFilters() nostr.Filters {
	var respfilters = make(nostr.Filters, 0, len(listeners)*2)

	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	// here we go through all the existing listeners
	for _, connlisteners := range listeners {
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

func setListener(id string, ws *WebSocket, filters nostr.Filters) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	subs, ok := listeners[ws]
	if !ok {
		subs = make(map[string]*Listener)
		listeners[ws] = subs
	}

	subs[id] = &Listener{
		filters: filters,
	}
}

// Remove a specific subscription id from listeners for a given ws client
func removeListenerId(ws *WebSocket, id string) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	subs, ok := listeners[ws]
	if ok {
		delete(listeners[ws], id)
		if len(subs) == 0 {
			delete(listeners, ws)
		}
	}
}

// Remove WebSocket conn from listeners
func removeListener(ws *WebSocket) {
	listenersMutex.Lock()
	defer listenersMutex.Unlock()

	_, ok := listeners[ws]
	if ok {
		delete(listeners, ws)
	}
}

func notifyListeners(event *nostr.Event) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	for ws, subs := range listeners {
		for id, listener := range subs {
			if !listener.filters.Match(event) {
				continue
			}
			ws.WriteJSON([]interface{}{"EVENT", id, event})
		}
	}
}
