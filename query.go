package main

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
)

func queryEvents(filter *filter.EventFilter) (events []event.Event, err error) {
	var conditions []string
	var params []interface{}

	if filter.ID != "" {
		conditions = append(conditions, "id = ?")
		params = append(params, filter.ID)
	}

	if filter.Author != "" {
		conditions = append(conditions, "pubkey = ?")
		params = append(params, filter.Author)
	}

	if filter.Kind != 0 {
		conditions = append(conditions, "kind = ?")
		params = append(params, filter.Kind)
	}

	if filter.Authors != nil {
		inkeys := make([]string, 0, len(filter.Authors))
		for _, key := range filter.Authors {
			// to prevent sql attack here we will check if
			// these keys are valid 32byte hex
			parsed, err := hex.DecodeString(key)
			if err != nil || len(parsed) != 32 {
				continue
			}
			inkeys = append(inkeys, fmt.Sprintf("'%x'", parsed))
		}
		conditions = append(conditions, `pubkey IN (`+strings.Join(inkeys, ",")+`)`)
	}

	if filter.TagEvent != "" {
		conditions = append(conditions, relatedEventsCondition)
		params = append(params, filter.TagEvent)
	}

	if filter.TagProfile != "" {
		conditions = append(conditions, relatedEventsCondition)
		params = append(params, filter.TagProfile)
	}

	if filter.Since != 0 {
		conditions = append(conditions, "created_at > ?")
		params = append(params, filter.Since)
	}

	if len(conditions) == 0 {
		// fallback
		conditions = append(conditions, "true")
	}

	query := db.Rebind("SELECT * FROM event WHERE " +
		strings.Join(conditions, " AND ") +
		"ORDER BY created_at LIMIT 100")

	err = db.Select(&events, query, params...)
	if err != nil && err != sql.ErrNoRows {
		log.Warn().Err(err).Interface("filter", filter).Msg("failed to fetch events")
		err = fmt.Errorf("failed to fetch events: %w", err)
	}

	return
}
