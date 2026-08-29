package store

import (
	"database/sql"
	"time"
)

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
func nullInt64(n int64) sql.NullInt64    { return sql.NullInt64{Int64: n, Valid: true} }

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

// nullTimeFromInterface converts the interface{} produced by sqlc STRFTIME
// expressions back to sql.NullTime. SQLite returns []byte or string for text.
func NullTimeFromInterface(v interface{}) sql.NullTime {
	if v == nil {
		return sql.NullTime{}
	}
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	default:
		return sql.NullTime{}
	}
	t, err := time.Parse(time.RFC3339, s)
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
