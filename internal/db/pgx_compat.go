package db

import (
	"github.com/jackc/pgx/v5/pgconn"
)

type dbCmdTag = pgconn.CommandTag
type dbFieldDesc = pgconn.FieldDescription
type dbStmtDesc = pgconn.StatementDescription
