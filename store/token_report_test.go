package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

// TestAddTokenUsage_ReportIDPartitionsBuckets ensures rows for the same (email, step, model, source)
// but different reportIDs land in distinct buckets — guards the new UNIQUE(user_email, date, step,
// model, source, report_id) constraint and the in-memory tokenBucket key.
func TestAddTokenUsage_ReportIDPartitionsBuckets(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("rpttoken")
	const step, model = "ReportSummary", "gemini-3-flash"

	if err := AddTokenUsage(email, step, model, "", ReportID(101), 1000, 200); err != nil {
		t.Fatalf("add report 101: %v", err)
	}
	if err := AddTokenUsage(email, step, model, "", ReportID(102), 500, 100); err != nil {
		t.Fatalf("add report 102: %v", err)
	}
	if err := AddTokenUsage(email, step, model, "", 0, 50, 10); err != nil {
		t.Fatalf("add unattributed: %v", err)
	}

	cost101, err := GetReportTokenUsage(context.Background(), ReportID(101))
	if err != nil {
		t.Fatalf("GetReportTokenUsage(101): %v", err)
	}
	if cost101.PromptTokens != 1000 || cost101.CompletionTokens != 200 || cost101.CallCount != 1 {
		t.Errorf("report 101 cost = %+v, want {1000, 200, 1}", cost101)
	}

	cost102, err := GetReportTokenUsage(context.Background(), ReportID(102))
	if err != nil {
		t.Fatalf("GetReportTokenUsage(102): %v", err)
	}
	if cost102.PromptTokens != 500 || cost102.CompletionTokens != 100 || cost102.CallCount != 1 {
		t.Errorf("report 102 cost = %+v, want {500, 100, 1}", cost102)
	}

	cost0, err := GetReportTokenUsage(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetReportTokenUsage(0): %v", err)
	}
	if cost0.PromptTokens != 50 || cost0.CompletionTokens != 10 || cost0.CallCount != 1 {
		t.Errorf("unattributed cost = %+v, want {50, 10, 1}", cost0)
	}
}

// TestGetReportTokenUsage_AggregatesAcrossSteps ensures the helper sums all report-bound steps
// (Summary + VizData + TranslateReport) into one cost figure rather than partitioning by step.
func TestGetReportTokenUsage_AggregatesAcrossSteps(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("rptsum")
	rid := ReportID(7777)

	cases := []struct {
		step, model string
		prompt, comp int
	}{
		{"ReportSummary", "gemini-3-flash", 8000, 1500},
		{"ReportVizData", "gemini-3-flash", 2000, 400},
		{"TranslateReport", "gemini-3-flash-lite", 6000, 800},
	}
	for _, c := range cases {
		if err := AddTokenUsage(email, c.step, c.model, "", rid, c.prompt, c.comp); err != nil {
			t.Fatalf("add %s: %v", c.step, err)
		}
	}

	cost, err := GetReportTokenUsage(context.Background(), rid)
	if err != nil {
		t.Fatalf("GetReportTokenUsage: %v", err)
	}
	wantP, wantC := 8000+2000+6000, 1500+400+800
	if cost.PromptTokens != wantP || cost.CompletionTokens != wantC || cost.CallCount != 3 {
		t.Errorf("aggregate = %+v, want {%d, %d, 3}", cost, wantP, wantC)
	}
}

// TestGetReportTokenUsage_IncludesInMemoryBuffers ensures un-flushed buckets are summed alongside
// DB rows — matches the GetDailyTokenUsage pattern so dashboards see real-time cost without a forced flush.
func TestGetReportTokenUsage_IncludesInMemoryBuffers(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("rptmem")
	rid := ReportID(8888)

	if err := AddTokenUsage(email, "ReportSummary", "gemini-3-flash", "", rid, 100, 20); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := FlushTokenUsage(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := AddTokenUsage(email, "ReportSummary", "gemini-3-flash", "", rid, 50, 10); err != nil {
		t.Fatalf("add un-flushed: %v", err)
	}

	cost, err := GetReportTokenUsage(context.Background(), rid)
	if err != nil {
		t.Fatalf("GetReportTokenUsage: %v", err)
	}
	if cost.PromptTokens != 150 || cost.CompletionTokens != 30 || cost.CallCount != 2 {
		t.Errorf("DB+memory aggregate = %+v, want {150, 30, 2}", cost)
	}
}
