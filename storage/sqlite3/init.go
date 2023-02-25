package sqlite3

import (
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	_ "github.com/mattn/go-sqlite3"
)

func (b *SQLite3Backend) Init() error {
	db, err := sqlx.Connect("sqlite3", b.DatabaseURL)
	if err != nil {
		return err
	}

	// sqlx default is 0 (unlimited), while sqlite3 by default accepts up to 100 connections
	db.SetMaxOpenConns(80)

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
