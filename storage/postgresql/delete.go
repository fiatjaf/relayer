package postgresql

func (b PostgresBackend) DeleteEvent(id string, pubkey string) error {
	_, err := b.DB.Exec("DELETE FROM event WHERE id = $1 AND pubkey = $2", id, pubkey)
	return err
}
