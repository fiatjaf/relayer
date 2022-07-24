package postgresql

import (
	"github.com/jmoiron/sqlx"
)

type PostgresBackend struct {
	*sqlx.DB
	DatabaseURL string
}
