package store

import (
	"strings"
	"testing"
)

// Why: a dropped save is only traceable if the line carries the identity that ties it
// back to the AI inference log — source_ts above all.
func TestFormatSaveDrop_CarriesIdentity(t *testing.T) {
	msg := ConsolidatedMessage{
		UserEmail: "u@x",
		Source:    SourceSlack,
		Room:      "biz-global-tech",
		SourceTS:  "1789546445.856419",
		Task:      "Attach HTTP call collection via hook_httpc_patterns and RemoteCall.x",
	}

	got := formatSaveDrop(msg, "source_ts already processed")

	for _, want := range []string{"source_ts already processed", "u@x", SourceSlack, "biz-global-tech", "1789546445.856419", "Attach HTTP call collection"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatSaveDrop() = %q, missing %q", got, want)
		}
	}
}

func TestFormatSaveDrop_TruncatesLongTask(t *testing.T) {
	long := strings.Repeat("가", 400)
	got := formatSaveDrop(ConsolidatedMessage{Task: long}, "dup cache")

	if len([]rune(got)) > 300 {
		t.Errorf("formatSaveDrop() rune length = %d, want <= 300", len([]rune(got)))
	}
}
