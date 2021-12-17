package main

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
