package relayer

import (
	"github.com/nbd-wtf/go-nostr"
)

func (s *Server) BroadcastEvent(evt *nostr.Event) {
	s.notifyListeners(evt)
}
