package relayer

import (
	"context"
	"fmt"
	"regexp"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
)

var nip20prefixmatcher = regexp.MustCompile(`^\w+: `)

// AddEvent has a business rule to add an event to the relayer
func (s *Server) AddEvent(ctx context.Context, evt *nostr.Event) (accepted bool, message string) {
	if evt == nil {
		return false, ""
	}

	store := s.relay.Storage(ctx)
	wrapper := &eventstore.RelayWrapper{
		Store: store,
	}
	advancedSaver, _ := store.(AdvancedSaver)

	if !s.relay.AcceptEvent(ctx, evt) {
		return false, "blocked: event blocked by relay"
	}

	if 20000 <= evt.Kind && evt.Kind < 30000 {
		// do not store ephemeral events
	} else {
		if advancedSaver != nil {
			advancedSaver.BeforeSave(ctx, evt)
		}

		if saveErr := wrapper.Publish(ctx, *evt); saveErr != nil {
			switch saveErr {
			case eventstore.ErrDupEvent:
				return true, saveErr.Error()
			default:
				errmsg := saveErr.Error()
				if nip20prefixmatcher.MatchString(errmsg) {
					return false, errmsg
				} else {
					return false, fmt.Sprintf("error: failed to save (%s)", errmsg)
				}
			}
		}

		if advancedSaver != nil {
			advancedSaver.AfterSave(evt)
		}
	}

	s.notifyListeners(evt)

	return true, ""
}
