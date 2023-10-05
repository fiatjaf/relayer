package sqlite3

import (
	"github.com/fiatjaf/relayer/v2"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	_ "github.com/mattn/go-sqlite3"
)

var _ relayer.Storage = (*SQLite3Backend)(nil)

var ddls = []string{
	`CREATE TABLE IF NOT EXISTS event (
       id text NOT NULL,
       pubkey text NOT NULL,
       created_at integer NOT NULL,
       kind integer NOT NULL,
       tags jsonb NOT NULL,
       content text NOT NULL,
       sig text NOT NULL);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ididx ON event(id)`,
	`CREATE INDEX IF NOT EXISTS pubkeyprefix ON event(pubkey)`,
	`CREATE INDEX IF NOT EXISTS timeidx ON event(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS kindidx ON event(kind)`,
}

func fixup(db *sqlx.DB) {
	row, err := db.Query(`SELECT id, rowid FROM event GROUP BY id HAVING COUNT(id) > 1`)
	if err == nil {
		for row.Next() {
			var id, rowid string
			err = row.Scan(&id, &rowid)
			if err != nil {
				continue
			}
			result, err := db.Exec(`DELETE FROM event WHERE id = ? AND rowid != ?`, id, rowid)
			if err != nil {
				continue
			}
			num, _ := result.RowsAffected()
			println(id, rowid, num)
		}
		row.Close()
		println("DONE")
	}
}

func (b *SQLite3Backend) Init() error {
	db, err := sqlx.Connect("sqlite3", b.DatabaseURL)
	if err != nil {
		return err
	}
	fixup(db)

	db.SetMaxOpenConns(b.MaxOpenConns)
	db.SetMaxIdleConns(b.MaxIdleConns)

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
	b.DB = db

	for _, ddl := range ddls {
		_, err = b.DB.Exec(ddl)
		if err != nil {
			return err
		}
	}
	return nil
}
