package store

import (
	"database/sql"
	"testing"
	"time"
)

func TestNullTimeToTime_Valid(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	nt := sql.NullTime{Time: ts, Valid: true}
	if got := nullTimeToTime(nt); !got.Equal(ts) {
		t.Errorf("nullTimeToTime valid = %v, want %v", got, ts)
	}
}

func TestNullTimeToTime_Invalid(t *testing.T) {
	t.Parallel()
	nt := sql.NullTime{Valid: false}
	if got := nullTimeToTime(nt); !got.IsZero() {
		t.Errorf("nullTimeToTime invalid = %v, want zero", got)
	}
}

func TestStringToNullString(t *testing.T) {
	t.Parallel()
	ns := stringToNullString("hello")
	if !ns.Valid || ns.String != "hello" {
		t.Errorf("stringToNullString('hello') = %+v, want {hello true}", ns)
	}
	ns = stringToNullString("")
	if ns.Valid {
		t.Error("stringToNullString('') should have Valid=false")
	}
}
