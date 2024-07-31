package relayer

import (
	"github.com/nbd-wtf/go-nostr"
)

func BroadcastEvent(evt *nostr.Event) {
	notifyListeners(evt)
}
