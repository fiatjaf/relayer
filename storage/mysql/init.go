package mysql

import (
	"strings"

	"github.com/fiatjaf/relayer/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

const (
	queryLimit        = 100
	queryIDsLimit     = 500
	queryAuthorsLimit = 500
	queryKindsLimit   = 10
	queryTagsLimit    = 10
)

var _ relayer.Storage = (*MySQLBackend)(nil)

var ddls = []string{
	`CREATE TABLE IF NOT EXISTS event (
       id char(64) NOT NULL primary key,
       pubkey char(64) NOT NULL,
       created_at int NOT NULL,
       kind integer NOT NULL,
       tags json NOT NULL,
       content text NOT NULL,
       sig text NOT NULL);`,
	`CREATE INDEX pubkeyprefix ON event (pubkey);`,
	`CREATE INDEX timeidx ON event (created_at DESC);`,
	`CREATE INDEX kindidx ON event (kind);`,
}

func (b *MySQLBackend) Init() error {
	db, err := sqlx.Connect("mysql", b.DatabaseURL)
	if err != nil {
		return err
	}

	// sqlx default is 0 (unlimited), while mysql by default accepts up to 100 connections
	db.SetMaxOpenConns(80)

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
	b.DB = db

	for _, ddl := range ddls {
		_, err := b.DB.Exec(ddl)
		if err != nil && !strings.HasPrefix(err.Error(), `Error 1061: Duplicate key name`) {
			return err
		}
	}

	if b.QueryLimit == 0 {
		b.QueryLimit = queryLimit
	}
	if b.QueryIDsLimit == 0 {
		b.QueryIDsLimit = queryIDsLimit
	}
	if b.QueryAuthorsLimit == 0 {
		b.QueryAuthorsLimit = queryAuthorsLimit
	}
	if b.QueryKindsLimit == 0 {
		b.QueryKindsLimit = queryKindsLimit
	}
	if b.QueryTagsLimit == 0 {
		b.QueryTagsLimit = queryTagsLimit
	}
	return err
}
