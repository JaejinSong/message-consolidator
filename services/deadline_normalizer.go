package services

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseDeadline normalizes a raw deadline string into an ISO YYYY-MM-DD date.
// ref is the message timestamp used to resolve relative expressions.
// Returns ("", false) when the string is empty or unparseable -- never invents a date.
func ParseDeadline(raw string, ref time.Time) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	if isoDateRe.MatchString(s) {
		return s, false
	}
	if d, ok := parseNatural(normalizeDeadlineText(s), ref); ok {
		return d.Format("2006-01-02"), true
	}
	return "", false
}

// deadlinePrefixes are leading markers that carry no date information.
var deadlinePrefixes = []string{
	"by ", "until ", "before ", "due ", "no later than ", "on ",
	"paling lambat ", "sebelum ", "deadline ", "the ",
}

// deadlineSuffixes are Korean particles that attach to the date phrase itself.
var deadlineSuffixes = []string{"까지", "안에", "이내", "중으로", "중에", "쯤", "경에", "경", "부로"}

// normalizeDeadlineText lowercases the phrase and strips the prefixes and Korean
// particles that surround a date expression without changing which date it names.
func normalizeDeadlineText(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.Trim(t, ".,!?:;()[]\"'")
	for _, p := range deadlinePrefixes {
		t = strings.TrimPrefix(t, p)
	}
	t = strings.Join(strings.Fields(t), " ")
	for _, sfx := range deadlineSuffixes {
		if trimmed := strings.TrimSuffix(t, sfx); trimmed != t {
			t = strings.TrimSpace(trimmed)
			break
		}
	}
	return strings.Join(strings.Fields(t), " ")
}

func parseNatural(s string, ref time.Time) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if d, ok := parseRelativeKeyword(s, ref); ok {
		return d, true
	}
	if d, ok := parseQualifiedWeekday(s, ref); ok {
		return d, true
	}
	if wd, ok := weekdayOf(s); ok {
		return nextWeekdayFrom(ref, wd), true
	}
	return parseAbsoluteDate(s, ref)
}

// parseRelativeKeyword resolves whole-phrase relative expressions in English,
// Korean and Indonesian.
func parseRelativeKeyword(s string, ref time.Time) (time.Time, bool) {
	switch s {
	case "today", "오늘", "eod", "end of day", "today eod", "hari ini":
		return ref, true
	case "tomorrow", "내일", "besok":
		return ref.AddDate(0, 0, 1), true
	case "this week", "이번 주", "이번주", "금주", "minggu ini", "end of week", "eow", "주중":
		return nextWeekdayFrom(ref, time.Friday), true
	case "next week", "다음 주", "다음주", "차주", "minggu depan":
		return startOfNextWeek(ref), true
	case "end of month", "eom", "월말", "이번 달 말", "akhir bulan":
		return endOfMonth(ref), true
	case "next month", "다음 달", "다음달", "내달", "bulan depan":
		return startOfNextMonth(ref), true
	}
	return time.Time{}, false
}

// nextWeekQualifiers name the week after the reference week; thisWeekQualifiers
// name the reference week and therefore keep the bare-weekday semantics.
var nextWeekQualifiers = []string{"다음주", "다음 주", "차주", "next week", "minggu depan"}
var thisWeekQualifiers = []string{"이번주", "이번 주", "금주", "minggu ini", "this week"}

// parseQualifiedWeekday resolves a weekday that carries an explicit week qualifier
// ("다음주 화요일", "kamis depan"). Bare "next friday" is deliberately left to
// weekdayOf so the pre-existing English semantics stay unchanged.
func parseQualifiedWeekday(s string, ref time.Time) (time.Time, bool) {
	for _, q := range nextWeekQualifiers {
		if rest, ok := trimQualifierPrefix(s, q); ok {
			if wd, found := weekdayOf(rest); found {
				return nextWeekdayFrom(startOfNextWeek(ref), wd), true
			}
		}
	}
	for _, q := range thisWeekQualifiers {
		if rest, ok := trimQualifierPrefix(s, q); ok {
			if wd, found := weekdayOf(rest); found {
				return nextWeekdayFrom(ref, wd), true
			}
		}
	}
	// Why: Indonesian places the qualifier after the weekday ("kamis depan").
	if rest, ok := trimQualifierSuffix(s, "depan"); ok {
		if wd, found := weekdayOf(rest); found {
			return nextWeekdayFrom(startOfNextWeek(ref), wd), true
		}
	}
	return time.Time{}, false
}

// trimQualifierPrefix accepts both the spaced and agglutinated Korean forms
// ("다음주 화요일" and "다음주화요일").
func trimQualifierPrefix(s, qualifier string) (string, bool) {
	if !strings.HasPrefix(s, qualifier) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, qualifier))
	if rest == "" {
		return "", false
	}
	return rest, true
}

func trimQualifierSuffix(s, qualifier string) (string, bool) {
	if !strings.HasSuffix(s, qualifier) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimSuffix(s, qualifier))
	if rest == "" {
		return "", false
	}
	return rest, true
}

// nextWeekdayFrom returns the next occurrence of wd on or after ref.
func nextWeekdayFrom(ref time.Time, wd time.Weekday) time.Time {
	days := (int(wd) - int(ref.Weekday()) + 7) % 7
	return ref.AddDate(0, 0, days)
}

// startOfNextWeek returns the Monday of the week after ref.
func startOfNextWeek(ref time.Time) time.Time {
	iso := int(ref.Weekday())
	if iso == 0 {
		iso = 7 // Why: time.Sunday is 0, but the week here starts on Monday.
	}
	return ref.AddDate(0, 0, 8-iso)
}

func startOfNextMonth(ref time.Time) time.Time {
	return time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, ref.Location()).AddDate(0, 1, 0)
}

func endOfMonth(ref time.Time) time.Time {
	return startOfNextMonth(ref).AddDate(0, 0, -1)
}

var weekdayNames = []struct {
	keys []string
	day  time.Weekday
}{
	{[]string{"monday", "mon", "월요일", "월", "senin"}, time.Monday},
	{[]string{"tuesday", "tue", "화요일", "화", "selasa"}, time.Tuesday},
	{[]string{"wednesday", "wed", "수요일", "수", "rabu"}, time.Wednesday},
	{[]string{"thursday", "thu", "목요일", "목", "kamis"}, time.Thursday},
	{[]string{"friday", "fri", "금요일", "금", "jumat", "jum'at"}, time.Friday},
	{[]string{"saturday", "sat", "토요일", "토", "sabtu"}, time.Saturday},
	{[]string{"sunday", "sun", "일요일", "일", "minggu"}, time.Sunday},
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

var (
	fullDateRe     = regexp.MustCompile(`^(\d{4})[./](\d{1,2})[./](\d{1,2})\.?$`)
	monthDayRe     = regexp.MustCompile(`^(\d{1,2})[/.](\d{1,2})\.?$`)
	koMonthDayRe   = regexp.MustCompile(`^(\d{1,2})월\s*(\d{1,2})일$`)
	dayOnlyRe      = regexp.MustCompile(`^(\d{1,2})(일|st|nd|rd|th)$`)
	monthNameRe    = regexp.MustCompile(`^([a-z]{3,9})\.?\s+(\d{1,2})(?:st|nd|rd|th)?$`)
	dayMonthNameRe = regexp.MustCompile(`^(\d{1,2})(?:st|nd|rd|th)?\s+([a-z]{3,9})\.?$`)
)

var monthPrefixes = []string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// parseAbsoluteDate resolves stated calendar dates that omit some component,
// filling year (and month for day-only forms) from ref.
func parseAbsoluteDate(s string, ref time.Time) (time.Time, bool) {
	if m := fullDateRe.FindStringSubmatch(s); m != nil {
		return buildDate(atoi(m[1]), atoi(m[2]), atoi(m[3]), ref)
	}
	if m := koMonthDayRe.FindStringSubmatch(s); m != nil {
		return buildDate(ref.Year(), atoi(m[1]), atoi(m[2]), ref)
	}
	if m := monthDayRe.FindStringSubmatch(s); m != nil {
		return buildDate(ref.Year(), atoi(m[1]), atoi(m[2]), ref)
	}
	if m := dayOnlyRe.FindStringSubmatch(s); m != nil {
		return buildDayOnly(atoi(m[1]), ref)
	}
	if m := monthNameRe.FindStringSubmatch(s); m != nil {
		if month, ok := monthOfName(m[1]); ok {
			return buildDate(ref.Year(), month, atoi(m[2]), ref)
		}
	}
	// Why: live mail and Slack carry both orders ("Sep 7" and "6 Sep", "04 Aug").
	if m := dayMonthNameRe.FindStringSubmatch(s); m != nil {
		if month, ok := monthOfName(m[2]); ok {
			return buildDate(ref.Year(), month, atoi(m[1]), ref)
		}
	}
	return time.Time{}, false
}

func monthOfName(name string) (int, bool) {
	for i, prefix := range monthPrefixes {
		if strings.HasPrefix(name, prefix) {
			return i + 1, true
		}
	}
	return 0, false
}

func buildDate(year, month, day int, ref time.Time) (time.Time, bool) {
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, ref.Location())
	if d.Day() != day || int(d.Month()) != month {
		return time.Time{}, false // Why: reject overflow like 2/30 rather than sliding it.
	}
	return d, true
}

// buildDayOnly resolves a bare day-of-month against ref, rolling into the next
// month when the day has already passed.
func buildDayOnly(day int, ref time.Time) (time.Time, bool) {
	d, ok := buildDate(ref.Year(), int(ref.Month()), day, ref)
	if !ok {
		return time.Time{}, false
	}
	if day < ref.Day() {
		next := startOfNextMonth(ref)
		return buildDate(next.Year(), int(next.Month()), day, ref)
	}
	return d, true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
