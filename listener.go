package relayer

import (
	"sync"

	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
	"github.com/gorilla/websocket"
)

type Listener struct {
	filters filter.EventFilters
}

var listeners = make(map[*websocket.Conn]map[string]*Listener)
var listenersMutex = sync.Mutex{}

func GetListeningFilters() filter.EventFilters {
	var respfilters = make(filter.EventFilters, 0, len(listeners)*2)

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
					if filter.Equal(listenerfilter, respfilter) {
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

func setListener(id string, conn *websocket.Conn, filters filter.EventFilters) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	subs, ok := listeners[conn]
	if !ok {
		subs = make(map[string]*Listener)
		listeners[conn] = subs
	}

	subs[id] = &Listener{
		filters: filters,
	}
}

func removeListener(conn *websocket.Conn, id string) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	subs, ok := listeners[conn]
	if ok {
		delete(listeners[conn], id)
		if len(subs) == 0 {
			delete(listeners, conn)
		}
	}
}

func notifyListeners(event *event.Event) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	for conn, subs := range listeners {
		for id, listener := range subs {
			if !listener.filters.Match(event) {
				continue
			}
			conn.WriteJSON([]interface{}{"EVENT", id, event})
		}
	}
}
