package mysql

import (
	"github.com/jmoiron/sqlx"
)

type MySQLBackend struct {
	*sqlx.DB
	DatabaseURL       string
	QueryLimit        int
	QueryIDsLimit     int
	QueryAuthorsLimit int
	QueryKindsLimit   int
	QueryTagsLimit    int
}
