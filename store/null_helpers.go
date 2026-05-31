package store

import (
	"database/sql"
	"time"
)

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
func nullInt64(n int64) sql.NullInt64    { return sql.NullInt64{Int64: n, Valid: true} }
func nullBool(b bool) sql.NullBool       { return sql.NullBool{Bool: b, Valid: true} }

func parseNullDate(iso string) sql.NullTime {
	if iso == "" {
		return sql.NullTime{}
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func boolToNullInt64(b bool) sql.NullInt64 {
	v := int64(0)
	if b {
		v = 1
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
