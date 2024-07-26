package relayer

import (
	"github.com/nbd-wtf/go-nostr"
)

// BroadcastEvent emits an event to all listeners whose filters' match, skipping all filters and actions
// it also doesn't attempt to store the event or trigger any reactions or callbacks
func (s *Server) BroadcastEvent(evt *nostr.Event) {
	s.notifyListeners(evt)
}
