package relayer

import (
	"github.com/nbd-wtf/go-nostr"
)

type Listener struct {
	filters nostr.Filters
}

func (s *Server) GetListeningFilters() nostr.Filters {
	respfilters := make(nostr.Filters, 0, len(s.listeners)*2)

	s.listenersMutex.Lock()
	defer s.listenersMutex.Unlock()

	// here we go through all the existing listeners
	for _, connlisteners := range s.listeners {
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

func (s *Server) setListener(id string, ws *WebSocket, filters nostr.Filters) {
	s.listenersMutex.Lock()
	defer s.listenersMutex.Unlock()

	subs, ok := s.listeners[ws]
	if !ok {
		subs = make(map[string]*Listener)
		s.listeners[ws] = subs
	}

	subs[id] = &Listener{filters: filters}
}

// Remove a specific subscription id from listeners for a given ws client
func (s *Server) removeListenerId(ws *WebSocket, id string) {
	s.listenersMutex.Lock()
	defer s.listenersMutex.Unlock()

	if subs, ok := s.listeners[ws]; ok {
		delete(s.listeners[ws], id)
		if len(subs) == 0 {
			delete(s.listeners, ws)
		}
	}
}

// Remove WebSocket conn from listeners
func (s *Server) removeListener(ws *WebSocket) {
	s.listenersMutex.Lock()
	defer s.listenersMutex.Unlock()
	clear(s.listeners[ws])
	delete(s.listeners, ws)
}

func (s *Server) notifyListeners(event *nostr.Event) {
	s.listenersMutex.Lock()
	defer s.listenersMutex.Unlock()

	for ws, subs := range s.listeners {
		for id, listener := range subs {
			if !listener.filters.Match(event) {
				continue
			}
			ws.WriteJSON(nostr.EventEnvelope{SubscriptionID: &id, Event: *event})
		}
	}
}
