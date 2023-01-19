package postgresql

import (
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	_ "github.com/lib/pq"
)

func (b *PostgresBackend) Init() error {
	db, err := sqlx.Connect("postgres", b.DatabaseURL)
	if err != nil {
		return err
	}

	// sqlx default is 0 (unlimited), while postgresql by default accepts up to 100 connections
	db.SetMaxOpenConns(80)

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
	b.DB = db

	_, err = b.DB.Exec(`
CREATE OR REPLACE FUNCTION tags_to_tagvalues(jsonb) RETURNS text[]
    AS 'SELECT array_agg(t->>1) FROM (SELECT jsonb_array_elements($1) AS t)s WHERE length(t->>0) = 1;'
    LANGUAGE SQL
    IMMUTABLE
    RETURNS NULL ON NULL INPUT;

CREATE TABLE IF NOT EXISTS event (
  id text NOT NULL,
  pubkey text NOT NULL,
  created_at integer NOT NULL,
  kind integer NOT NULL,
  tags jsonb NOT NULL,
  content text NOT NULL,
  sig text NOT NULL,

  tagvalues text[] GENERATED ALWAYS AS (tags_to_tagvalues(tags)) STORED
);

CREATE UNIQUE INDEX IF NOT EXISTS ididx ON event USING btree (id text_pattern_ops);
CREATE INDEX IF NOT EXISTS pubkeyprefix ON event USING btree (pubkey text_pattern_ops);
CREATE INDEX IF NOT EXISTS timeidx ON event (created_at DESC);
CREATE INDEX IF NOT EXISTS kindidx ON event (kind);
CREATE INDEX IF NOT EXISTS arbitrarytagvalues ON event USING gin (tagvalues);
    `)
	return err
}
