package postgresql

import (
	"context"
	"encoding/json"

	"github.com/fiatjaf/relayer/v2/storage"
	"github.com/nbd-wtf/go-nostr"
)

func (b *PostgresBackend) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	deleteQuery, deleteParams, shouldDelete := deleteBeforeSaveSql(evt)
	if shouldDelete {
		_, _ = b.DB.ExecContext(ctx, deleteQuery, deleteParams...)
	}

	sql, params := saveEventSql(evt)
	res, err := b.DB.ExecContext(ctx, sql, params...)
	if err != nil {
		return err
	}

	nr, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if nr == 0 {
		return storage.ErrDupEvent
	}

	return nil
}

func (b *PostgresBackend) BeforeSave(ctx context.Context, evt *nostr.Event) {
	// do nothing
}

func (b *PostgresBackend) AfterSave(evt *nostr.Event) {
	// delete all but the 100 most recent ones for each key
	b.DB.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND created_at < (
      SELECT created_at FROM event WHERE pubkey = $1
      ORDER BY created_at DESC OFFSET 100 LIMIT 1
    )`, evt.PubKey, evt.Kind)
}

func deleteBeforeSaveSql(evt *nostr.Event) (string, []any, bool) {
	// react to different kinds of events
	var (
		query        = ""
		params       []any
		shouldDelete bool
	)
	if evt.Kind == nostr.KindSetMetadata || evt.Kind == nostr.KindContactList || (10000 <= evt.Kind && evt.Kind < 20000) {
		// delete past events from this user
		query = `DELETE FROM event WHERE pubkey = $1 AND kind = $2`
		params = []any{evt.PubKey, evt.Kind}
		shouldDelete = true
	} else if evt.Kind == nostr.KindRecommendServer {
		// delete past recommend_server events equal to this one
		query = `DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND content = $3`
		params = []any{evt.PubKey, evt.Kind, evt.Content}
		shouldDelete = true
	} else if evt.Kind >= 30000 && evt.Kind < 40000 {
		// NIP-33
		d := evt.Tags.GetFirst([]string{"d"})
		if d != nil {
			query = `DELETE FROM event WHERE pubkey = $1 AND kind = $2 AND tagvalues && ARRAY[$3]`
			params = []any{evt.PubKey, evt.Kind, d.Value()}
			shouldDelete = true
		}
	}

	return query, params, shouldDelete
}

func saveEventSql(evt *nostr.Event) (string, []any) {
	const query = `INSERT INTO event (id, pubkey, created_at, kind, tags, content, sig) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`
	tagsj, _ := json.Marshal(evt.Tags)
	params := []any{evt.ID, evt.PubKey, evt.CreatedAt, evt.Kind, tagsj, evt.Content, evt.Sig}
	return query, params
}
