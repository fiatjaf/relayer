package postgresql

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/stretchr/testify/assert"
)

func TestDeleteBeforeSave(t *testing.T) {
	var tests = []struct {
		name         string
		event        *nostr.Event
		query        string
		params       []any
		shouldDelete bool
	}{
		{
			name: "set metadata",
			event: &nostr.Event{
				Kind:   nostr.KindSetMetadata,
				PubKey: "pk",
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2",
			params:       []any{"pk", nostr.KindSetMetadata},
			shouldDelete: true,
		},
		{
			name: "contact list",
			event: &nostr.Event{
				Kind:   nostr.KindContactList,
				PubKey: "pk",
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2",
			params:       []any{"pk", nostr.KindContactList},
			shouldDelete: true,
		},
		{
			name: "recommend server",
			event: &nostr.Event{
				Kind:    nostr.KindRecommendServer,
				PubKey:  "pk",
				Content: "test",
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND content = $3",
			params:       []any{"pk", nostr.KindRecommendServer, "test"},
			shouldDelete: true,
		},
		{
			name: "nip-33",
			event: &nostr.Event{
				Kind:   31000,
				PubKey: "pk",
				Tags:   nostr.Tags{nostr.Tag{"d", "value"}},
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND tagvalues && ARRAY[$3]",
			params:       []any{"pk", 31000, "value"},
			shouldDelete: true,
		},
		{
			name: "kind > 10000",
			event: &nostr.Event{
				Kind:   10001,
				PubKey: "pk",
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2",
			params:       []any{"pk", 10001},
			shouldDelete: true,
		},
		{
			name: "kind < 20000",
			event: &nostr.Event{
				Kind:   19999,
				PubKey: "pk",
			},
			query:        "DELETE FROM event WHERE pubkey = $1 AND kind = $2",
			params:       []any{"pk", 19999},
			shouldDelete: true,
		},
		// Should not delete cases
		{
			name: "kind < 10000",
			event: &nostr.Event{
				Kind:   9999,
				PubKey: "pk",
			},
			query:        "",
			params:       nil,
			shouldDelete: false,
		},
		{
			name: "kind > 21000",
			event: &nostr.Event{
				Kind:   21000,
				PubKey: "pk",
			},
			query:        "",
			params:       nil,
			shouldDelete: false,
		},
		{
			name: "kind 1",
			event: &nostr.Event{
				Kind:   nostr.KindTextNote,
				PubKey: "pk",
			},
			query:        "",
			params:       nil,
			shouldDelete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, params, shouldDelete := deleteBeforeSaveSql(tt.event)
			assert.Equal(t, tt.query, query)
			assert.Equal(t, tt.params, params)
			assert.Equal(t, tt.shouldDelete, shouldDelete)
		})
	}
}

func TestSaveEventSql(t *testing.T) {
	now := nostr.Now()
	tests := []struct {
		name   string
		event  *nostr.Event
		query  string
		params []any
	}{
		{
			name: "basic",
			event: &nostr.Event{
				ID:        "id",
				PubKey:    "pk",
				CreatedAt: now,
				Kind:      nostr.KindTextNote,
				Content:   "test",
				Sig:       "sig",
			},
			query:  `INSERT INTO event (id, pubkey, created_at, kind, tags, content, sig) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
			params: []any{"id", "pk", now, nostr.KindTextNote, []byte("null"), "test", "sig"},
		},
		{
			name: "tags",
			event: &nostr.Event{
				ID:        "id",
				PubKey:    "pk",
				CreatedAt: now,
				Kind:      nostr.KindTextNote,
				Tags:      nostr.Tags{nostr.Tag{"foo", "bar"}},
				Content:   "test",
				Sig:       "sig",
			},
			query:  `INSERT INTO event (id, pubkey, created_at, kind, tags, content, sig) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
			params: []any{"id", "pk", now, nostr.KindTextNote, []byte("[[\"foo\",\"bar\"]]"), "test", "sig"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, params := saveEventSql(tt.event)
			assert.Equal(t, clean(tt.query), clean(query))
			assert.Equal(t, tt.params, params)
		})
	}
}
