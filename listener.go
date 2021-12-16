package main

import (
	"sync"

	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
	"github.com/gorilla/websocket"
)

type Listener struct {
	ws      *websocket.Conn
	filters filter.EventFilters
}

var listeners = make(map[string]*Listener)
var listenersMutex = sync.Mutex{}

func setListener(id string, conn *websocket.Conn, filters filter.EventFilters) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	listeners[id] = &Listener{
		ws:      conn,
		filters: filters,
	}
}

func removeListener(id string) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	delete(listeners, id)
}

func notifyListeners(event *event.Event) {
	listenersMutex.Lock()
	defer func() {
		listenersMutex.Unlock()
	}()

	for id, listener := range listeners {
		if !listener.filters.Match(event) {
			continue
		}

		listener.ws.WriteJSON([]interface{}{"EVENT", id, event})
	}
}
