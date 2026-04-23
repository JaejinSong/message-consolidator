package store

import "database/sql"

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
func nullInt64(n int64) sql.NullInt64    { return sql.NullInt64{Int64: n, Valid: true} }
func nullBool(b bool) sql.NullBool       { return sql.NullBool{Bool: b, Valid: true} }
