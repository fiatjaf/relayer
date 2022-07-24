package main

import (
	"fmt"

	"github.com/fiatjaf/relayer"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/kelseyhightower/envconfig"
)

type ClosedRelay struct {
	PostgresDatabase  string   `envconfig:"POSTGRESQL_DATABASE"`
	AuthorizedPubkeys []string `envconfig:"AUTHORIZED_PUBKEYS"`

	DB *sqlx.DB
}

func (b *ClosedRelay) Name() string {
	return "ClosedRelay"
}

func (b *ClosedRelay) Init() error {
	err := envconfig.Process("", b)
	if err != nil {
		return fmt.Errorf("couldn't process envconfig: %w", err)
	}

	if db, err := initDB(b.PostgresDatabase); err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	} else {
		db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
		b.DB = db
	}

	return nil
}

func main() {
	var b ClosedRelay

	relayer.Start(&b)
}
