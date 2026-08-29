package handlers

import (
	"net/url"
	"testing"
	"time"
)

func TestParseWAPaging(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                  string
		query                 string
		wantLimit, wantOffset int64
	}{
		{"defaults when absent", "", waDefaultLimit, 0},
		{"explicit values pass through", "limit=42&offset=7", 42, 7},
		{"zero limit falls back to default", "limit=0", waDefaultLimit, 0},
		{"negative limit falls back to default", "limit=-5", waDefaultLimit, 0},
		{"limit clamped to max", "limit=99999", waMaxLimit, 0},
		{"limit at max is kept", "limit=500", waMaxLimit, 0},
		{"unparseable limit falls back to default", "limit=abc", waDefaultLimit, 0},
		{"negative offset floors at zero", "offset=-3", waDefaultLimit, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("bad test query: %v", err)
			}
			limit, offset := parseWAPaging(q)
			if limit != tc.wantLimit || offset != tc.wantOffset {
				t.Errorf("parseWAPaging(%q) = limit %d/offset %d, want %d/%d",
					tc.query, limit, offset, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

func TestParseWATimeRange_DateShorthand(t *testing.T) {
	t.Parallel()
	q := url.Values{"date": {"2026-08-26"}}
	fromTs, toTs, err := parseWATimeRange(q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	start := time.Date(2026, 8, 26, 0, 0, 0, 0, seoulLoc)
	if fromTs != start.Unix() {
		t.Errorf("fromTs = %d, want %d (Seoul midnight)", fromTs, start.Unix())
	}
	// The window must end one second short of the next midnight, not spill into it.
	if want := start.Add(24*time.Hour - time.Second).Unix(); toTs != want {
		t.Errorf("toTs = %d, want %d", toTs, want)
	}
	if toTs-fromTs != 86399 {
		t.Errorf("window spans %d seconds, want 86399", toTs-fromTs)
	}
}

// A malformed date is the caller's entire filter, so it must be rejected rather than
// silently widening the query to everything.
func TestParseWATimeRange_BadDateRejected(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"26-08-2026", "2026/08/26", "not-a-date", "2026-13-01"} {
		_, _, err := parseWATimeRange(url.Values{"date": {bad}})
		if err == nil {
			t.Errorf("date=%q should be rejected", bad)
		}
	}
}

func TestParseWATimeRange_FromTo(t *testing.T) {
	t.Parallel()
	from := "2026-08-01T00:00:00Z"
	to := "2026-08-31T23:59:59Z"
	fromTs, toTs, err := parseWATimeRange(url.Values{"from": {from}, "to": {to}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantFrom, _ := time.Parse(time.RFC3339, from)
	wantTo, _ := time.Parse(time.RFC3339, to)
	if fromTs != wantFrom.Unix() || toTs != wantTo.Unix() {
		t.Errorf("got %d/%d, want %d/%d", fromTs, toTs, wantFrom.Unix(), wantTo.Unix())
	}
}

// Unlike date, an unparseable from/to leaves that bound open instead of failing.
func TestParseWATimeRange_BadBoundsLeaveOpen(t *testing.T) {
	t.Parallel()
	fromTs, toTs, err := parseWATimeRange(url.Values{"from": {"garbage"}, "to": {"garbage"}})
	if err != nil {
		t.Fatalf("from/to must not error, got %v", err)
	}
	if fromTs != 0 || toTs != 0 {
		t.Errorf("got %d/%d, want both bounds left at 0", fromTs, toTs)
	}
}

// date wins over from/to when both are supplied.
func TestParseWATimeRange_DateTakesPrecedence(t *testing.T) {
	t.Parallel()
	q := url.Values{"date": {"2026-08-26"}, "from": {"2020-01-01T00:00:00Z"}}
	fromTs, _, err := parseWATimeRange(q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := time.Date(2026, 8, 26, 0, 0, 0, 0, seoulLoc).Unix(); fromTs != want {
		t.Errorf("fromTs = %d, want %d (date should win over from)", fromTs, want)
	}
}

func TestParseWADirection(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"incoming": "incoming",
		"outgoing": "outgoing",
		"INCOMING": "incoming",
		"OutGoing": "outgoing",
		"sideways": "",
		"":         "",
	}
	for in, want := range cases {
		if got := parseWADirection(url.Values{"direction": {in}}); got != want {
			t.Errorf("parseWADirection(%q) = %q, want %q", in, got, want)
		}
	}
}
