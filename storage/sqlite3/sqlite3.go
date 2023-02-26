package sqlite3

import (
	"github.com/jmoiron/sqlx"
)

type SQLite3Backend struct {
	*sqlx.DB
	DatabaseURL string
}
