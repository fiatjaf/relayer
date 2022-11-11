package relayer

import (
	"fmt"

	"github.com/nbd-wtf/go-nostr"
)

func AddEvent(relay Relay, evt nostr.Event) (accepted bool, message string) {
	store := relay.Storage()
	advancedDeleter, _ := store.(AdvancedDeleter)
	advancedSaver, _ := store.(AdvancedSaver)

	if evt.Kind == 5 {
		// event deletion -- nip09
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				if advancedDeleter != nil {
					advancedDeleter.BeforeDelete(tag[1], evt.PubKey)
				}

				if err := store.DeleteEvent(tag[1], evt.PubKey); err != nil {
					return false, fmt.Sprintf("error: failed to delete: %s", err.Error())
				}

				if advancedDeleter != nil {
					advancedDeleter.AfterDelete(tag[1], evt.PubKey)
				}
			}
		}
		return true, ""
	}

	if !relay.AcceptEvent(&evt) {
		return false, "blocked"
	}

	if 20000 <= evt.Kind && evt.Kind < 30000 {
		// do not store ephemeral events
	} else {
		if advancedSaver != nil {
			advancedSaver.BeforeSave(&evt)
		}

		if err := store.SaveEvent(&evt); err != nil {
			return false, fmt.Sprintf("error: failed to save: %s", err.Error())
		}

		if advancedSaver != nil {
			advancedSaver.AfterSave(&evt)
		}
	}

	notifyListeners(&evt)

	return true, ""
}
