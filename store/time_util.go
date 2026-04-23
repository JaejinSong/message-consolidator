package store

import "time"

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

