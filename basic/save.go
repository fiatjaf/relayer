package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fiatjaf/go-nostr"
)

func (b *BasicRelay) SaveEvent(evt *nostr.Event) error {
	// disallow large contents
	if len(evt.Content) > 1000 {
		return errors.New("event content too large")
	}

	// react to different kinds of events
	switch evt.Kind {
	case nostr.KindSetMetadata:
		// delete past set_metadata events from this user
		b.DB.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 0`, evt.PubKey)
	case nostr.KindRecommendServer:
		// delete past recommend_server events equal to this one
		b.DB.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 2 AND content = $2`,
			evt.PubKey, evt.Content)
	case nostr.KindContactList:
		// delete past contact lists from this same pubkey
		b.DB.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 3`, evt.PubKey)
	default:
		// delete all but the 10 most recent ones
		b.DB.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND created_at < (
          SELECT created_at FROM event WHERE pubkey = $1
          ORDER BY created_at DESC OFFSET 10 LIMIT 1
        )`,
			evt.PubKey, evt.Kind)
	}

	// insert
	tagsj, _ := json.Marshal(evt.Tags)
	_, err := b.DB.Exec(`
        INSERT INTO event (id, pubkey, created_at, kind, tags, content, sig)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, evt.ID, evt.PubKey, evt.CreatedAt.Unix(), evt.Kind, tagsj, evt.Content, evt.Sig)
	if err != nil {
		if strings.Index(err.Error(), "UNIQUE") != -1 {
			// already exists
			return nil
		}

		return fmt.Errorf("failed to save event %s: %w", evt.ID, err)
	}

	return nil
}
