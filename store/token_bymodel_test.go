package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestGetMonthlyTokenUsageByModel(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "bymodel-monthly@example.com" // unique: in-memory token buffers are not reset between tests

	// Two flushed rows for two models + one in-memory delta for the first model.
	// Trailing arg is cached_tokens (DeepSeek prompt-cache-hit subset of prompt).
	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 1000, 200, 50, 300)
	_ = AddTokenUsage(email, "ReportSummary", "deepseek-v4-pro", "", 0, 2000, 500, 300, 0)
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("FlushTokenUsage: %v", err)
	}
	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 100, 20, 5, 10) // in-memory only

	rows, err := GetMonthlyTokenUsageByModel(ctx, email)
	if err != nil {
		t.Fatalf("GetMonthlyTokenUsageByModel: %v", err)
	}

	byModel := make(map[string]ModelTokenUsage, len(rows))
	for _, r := range rows {
		byModel[r.Model] = r
	}

	chat, ok := byModel["deepseek-chat"]
	if !ok {
		t.Fatalf("missing deepseek-chat row in %+v", rows)
	}
	// DB (1000/200/50, cached 300) + in-memory (100/20/5, cached 10)
	if chat.Prompt != 1100 || chat.Completion != 220 || chat.Thinking != 55 || chat.Cached != 310 {
		t.Errorf("deepseek-chat = %+v, want prompt 1100 completion 220 thinking 55 cached 310", chat)
	}

	pro, ok := byModel["deepseek-v4-pro"]
	if !ok {
		t.Fatalf("missing deepseek-v4-pro row in %+v", rows)
	}
	if pro.Prompt != 2000 || pro.Completion != 500 || pro.Thinking != 300 {
		t.Errorf("deepseek-v4-pro = %+v, want prompt 2000 completion 500 thinking 300", pro)
	}

	// Why: in-memory token buffers are package globals not cleared by ResetForTest; drain the
	// un-flushed delta so it can't leak into report-cost aggregation in subsequent tests.
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("final FlushTokenUsage: %v", err)
	}
}

func TestGetDailyTokenUsageByModelEmpty(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	rows, err := GetDailyTokenUsageByModel(context.Background(), "bymodel-daily-empty@example.com")
	if err != nil {
		t.Fatalf("GetDailyTokenUsageByModel: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected no rows for fresh email, got %+v", rows)
	}
}
