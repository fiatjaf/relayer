package postgresql

import "context"

func (b PostgresBackend) DeleteEvent(ctx context.Context, id string, pubkey string) error {
	_, err := b.DB.ExecContext(ctx, "DELETE FROM event WHERE id = $1 AND pubkey = $2", id, pubkey)
	return err
}
