package sqlite3

import (
	"github.com/fiatjaf/relayer/v2"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	_ "github.com/mattn/go-sqlite3"
)

var _ relayer.Storage = (*SQLite3Backend)(nil)

func (b *SQLite3Backend) Init() error {
	db, err := sqlx.Connect("sqlite3", b.DatabaseURL)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(b.MaxOpenConns)
	db.SetMaxIdleConns(b.MaxIdleConns)

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
	b.DB = db

	_, err = b.DB.Exec(`
CREATE TABLE IF NOT EXISTS event (
  id text NOT NULL,
  pubkey text NOT NULL,
  created_at integer NOT NULL,
  kind integer NOT NULL,
  tags jsonb NOT NULL,
  content text NOT NULL,
  sig text NOT NULL
);
    `)
	return err
}
