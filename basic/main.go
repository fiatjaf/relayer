package main

import (
	"fmt"

	"github.com/fiatjaf/relayer"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/kelseyhightower/envconfig"
)

type BasicRelay struct {
	PostgresDatabase string `envconfig:"POSTGRESQL_DATABASE"`

	DB *sqlx.DB
}

func (b *BasicRelay) Name() string {
	return "BasicRelay"
}

func (b *BasicRelay) Init() error {
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

	go cleanupRoutine(b.DB)

	return nil
}

func main() {
	var b BasicRelay

	relayer.Start(&b)
}
