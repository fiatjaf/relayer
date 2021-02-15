package main

import (
	"sync"

	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
	"github.com/gorilla/websocket"
)

type Listener struct {
	ws      *websocket.Conn
	filters []*filter.EventFilter
}

var listeners = make(map[string]*Listener)
var listenersMutex = sync.Mutex{}

func setListener(id string, conn *websocket.Conn, filters []*filter.EventFilter) {
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
		match := false
		for _, filter := range listener.filters {
			if filter.Matches(event) {
				match = true
				break
			}
		}

		if !match {
			continue
		}

		listener.ws.WriteJSON([]interface{}{"EVENT", id, event})
	}
}
