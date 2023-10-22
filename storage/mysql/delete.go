package mysql

import "context"

func (b MySQLBackend) DeleteEvent(ctx context.Context, id string, pubkey string) error {
	_, err := b.DB.ExecContext(ctx, "DELETE FROM event WHERE id = ? AND pubkey = ?", id, pubkey)
	return err
}
