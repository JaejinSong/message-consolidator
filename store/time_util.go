package store

import (
	"fmt"
	"time"
)

//Why: Converts an IANA timezone string into a SQLite-compatible offset modifier to ensure correct date-time calculations in database queries.
func GetSQLiteOffset(userTz string) string {
	if userTz == "" || userTz == "UTC" {
		return "+00:00"
	}

	loc, err := time.LoadLocation(userTz)
	if err != nil {
		//Why: Defaults to UTC if the provided timezone is invalid or unrecognized.
		return "+00:00"
	}

	//Why: Formats the current time's offset for the specified location to be used as a SQLite modifier.
	return time.Now().In(loc).Format("-07:00")
}

// GetWorkingDaysAgo calculates the time 'days' working days ago from 'now'.
func GetWorkingDaysAgo(days int, now time.Time) time.Time {
	t := now
	for i := 0; i < days; {
		t = t.AddDate(0, 0, -1)
		if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
			i++
		}
	}
	return t
}

// GetLocalThreshold returns a threshold string formatted in RFC3339 for 'days' working days ago
// considering the user's timezone.
func GetLocalThreshold(userTz string, days int) string {
	now := time.Now()
	if loc, err := time.LoadLocation(userTz); err == nil {
		now = now.In(loc)
	}
	return GetWorkingDaysAgo(days, now).Format(time.RFC3339)
}

// DBTime handles scanning both time.Time and string types from database.
type DBTime struct {
	Time  time.Time
	Valid bool
}

func (d *DBTime) Scan(value interface{}) error {
	if value == nil {
		d.Time, d.Valid = time.Time{}, false
		return nil
	}
	d.Valid = true
	switch v := value.(type) {
	case time.Time:
		d.Time = v
		return nil
	case string:
		d.Time = ParseDBTimeString(v)
		return nil
	case []byte:
		d.Time = ParseDBTimeString(string(v))
		return nil
	}
	return fmt.Errorf("cannot scan type %T into DBTime", value)
}

// ParseDBTimeString attempts to parse a string into a time.Time using various formats.
func ParseDBTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
