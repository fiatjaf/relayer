package postgresql

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func (b PostgresBackend) QueryEvents(filter *nostr.Filter) (events []nostr.Event, err error) {
	var conditions []string
	var params []any

	if filter == nil {
		err = errors.New("filter cannot be null")
		return
	}

	if filter.IDs != nil {
		if len(filter.IDs) > 500 {
			// too many ids, fail everything
			return
		}

		likeids := make([]string, 0, len(filter.IDs))
		for _, id := range filter.IDs {
			// to prevent sql attack here we will check if
			// these ids are valid 32byte hex
			parsed, err := hex.DecodeString(id)
			if err != nil || len(parsed) != 32 {
				continue
			}
			likeids = append(likeids, fmt.Sprintf("id LIKE '%x%%'", parsed))
		}
		if len(likeids) == 0 {
			// ids being [] mean you won't get anything
			return
		}
		conditions = append(conditions, "("+strings.Join(likeids, " OR ")+")")
	}

	if filter.Authors != nil {
		if len(filter.Authors) > 500 {
			// too many authors, fail everything
			return
		}

		likekeys := make([]string, 0, len(filter.Authors))
		for _, key := range filter.Authors {
			// to prevent sql attack here we will check if
			// these keys are valid 32byte hex
			parsed, err := hex.DecodeString(key)
			if err != nil || len(parsed) != 32 {
				continue
			}
			likekeys = append(likekeys, fmt.Sprintf("pubkey LIKE '%x%%'", parsed))
		}
		if len(likekeys) == 0 {
			// authors being [] mean you won't get anything
			return
		}
		conditions = append(conditions, "("+strings.Join(likekeys, " OR ")+")")
	}

	if filter.Kinds != nil {
		if len(filter.Kinds) > 10 {
			// too many kinds, fail everything
			return
		}

		if len(filter.Kinds) == 0 {
			// kinds being [] mean you won't get anything
			return
		}
		// no sql injection issues since these are ints
		inkinds := make([]string, len(filter.Kinds))
		for i, kind := range filter.Kinds {
			inkinds[i] = strconv.Itoa(kind)
		}
		conditions = append(conditions, `kind IN (`+strings.Join(inkinds, ",")+`)`)
	}

	tagQuery := make([]string, 0, 1)
	for _, values := range filter.Tags {
		if len(values) == 0 {
			// any tag set to [] is wrong
			return
		}

		// add these tags to the query
		tagQuery = append(tagQuery, values...)

		if len(tagQuery) > 10 {
			// too many tags, fail everything
			return
		}
	}

	if len(tagQuery) > 0 {
		arrayBuild := make([]string, len(tagQuery))
		for i, tagValue := range tagQuery {
			arrayBuild[i] = "?"
			params = append(params, tagValue)
		}

		// we use a very bad implementation in which we only check the tag values and
		// ignore the tag names
		conditions = append(conditions,
			"tagvalues && ARRAY["+strings.Join(arrayBuild, ",")+"]")
	}

	if filter.Since != nil {
		conditions = append(conditions, "created_at > ?")
		params = append(params, filter.Since.Unix())
	}
	if filter.Until != nil {
		conditions = append(conditions, "created_at < ?")
		params = append(params, filter.Until.Unix())
	}

	if len(conditions) == 0 {
		// fallback
		conditions = append(conditions, "true")
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		params = append(params, 100)
	} else {
		params = append(params, filter.Limit)
	}

	query := b.DB.Rebind(`SELECT
      id, pubkey, created_at, kind, tags, content, sig
    FROM event WHERE ` +
		strings.Join(conditions, " AND ") +
		" ORDER BY created_at DESC LIMIT ?")

	rows, err := b.DB.Query(query, params...)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to fetch events using query %q: %w", query, err)
	}

	defer rows.Close()

	for rows.Next() {
		var evt nostr.Event
		var timestamp int64
		err := rows.Scan(&evt.ID, &evt.PubKey, &timestamp,
			&evt.Kind, &evt.Tags, &evt.Content, &evt.Sig)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		evt.CreatedAt = time.Unix(timestamp, 0)
		events = append(events, evt)
	}

	return events, nil
}
