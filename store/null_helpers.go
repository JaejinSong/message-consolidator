package store

import (
	"database/sql"
	"time"
)

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
func nullInt64(n int64) sql.NullInt64    { return sql.NullInt64{Int64: n, Valid: true} }
func nullTime(t time.Time) sql.NullTime  { return sql.NullTime{Time: t, Valid: true} }
func nullBool(b bool) sql.NullBool       { return sql.NullBool{Bool: b, Valid: true} }
