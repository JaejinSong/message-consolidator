package services

import (
	"regexp"
	"strings"
	"time"
)

var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseDeadline normalizes a raw deadline string into an ISO YYYY-MM-DD date.
// ref is the message timestamp used to resolve relative expressions.
// Returns ("", false) when the string is empty or unparseable — never invents a date.
func ParseDeadline(raw string, ref time.Time) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	if isoDateRe.MatchString(s) {
		return s, false
	}
	if d, ok := parseNatural(strings.ToLower(s), ref); ok {
		return d.Format("2006-01-02"), true
	}
	return "", false
}

func parseNatural(s string, ref time.Time) (time.Time, bool) {
	// strip common prefixes: "by ", "until ", "before "
	for _, pfx := range []string{"by ", "until ", "before ", "due "} {
		s = strings.TrimPrefix(s, pfx)
	}

	switch s {
	case "today", "오늘", "eod", "end of day", "today eod":
		return ref, true
	case "tomorrow", "내일":
		return ref.AddDate(0, 0, 1), true
	case "this week", "이번 주", "이번주":
		return nextWeekdayFrom(ref, time.Friday), true
	case "next week", "다음 주", "다음주":
		return ref.AddDate(0, 0, 7-int(ref.Weekday())+1), true // next Monday
	}

	if wd, ok := weekdayOf(s); ok {
		return nextWeekdayFrom(ref, wd), true
	}

	return time.Time{}, false
}

// nextWeekdayFrom returns the next occurrence of wd on or after ref.
func nextWeekdayFrom(ref time.Time, wd time.Weekday) time.Time {
	days := (int(wd) - int(ref.Weekday()) + 7) % 7
	return ref.AddDate(0, 0, days)
}

var weekdayNames = []struct {
	keys []string
	day  time.Weekday
}{
	{[]string{"monday", "mon", "월요일", "월"}, time.Monday},
	{[]string{"tuesday", "tue", "화요일", "화"}, time.Tuesday},
	{[]string{"wednesday", "wed", "수요일", "수"}, time.Wednesday},
	{[]string{"thursday", "thu", "목요일", "목"}, time.Thursday},
	{[]string{"friday", "fri", "금요일", "금"}, time.Friday},
	{[]string{"saturday", "sat", "토요일", "토"}, time.Saturday},
	{[]string{"sunday", "sun", "일요일", "일"}, time.Sunday},
}

func weekdayOf(s string) (time.Weekday, bool) {
	// strip "next " prefix before matching
	bare := strings.TrimPrefix(s, "next ")
	bare = strings.TrimPrefix(bare, "다음 ")
	for _, entry := range weekdayNames {
		for _, key := range entry.keys {
			if bare == key {
				return entry.day, true
			}
		}
	}
	return 0, false
}
