package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
	"time"
)

// TestGetDailyTokenUsage_DBAndInMemoryMerge pins the DB-plus-buffered-delta merge path:
// one flushed row plus one un-flushed AddTokenUsage call must sum together.
func TestGetDailyTokenUsage_DBAndInMemoryMerge(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "daily-merge@example.com" // unique: in-memory token buffers are not reset between tests

	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 1000, 200, 50, 0)
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("FlushTokenUsage: %v", err)
	}
	IncrementFilteredCount(email)
	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 100, 20, 5, 0) // in-memory only

	prompt, completion, thinking, filtered, err := GetDailyTokenUsage(ctx, email)
	if err != nil {
		t.Fatalf("GetDailyTokenUsage: %v", err)
	}
	if prompt != 1100 || completion != 220 || thinking != 55 || filtered != 1 {
		t.Errorf("got prompt=%d completion=%d thinking=%d filtered=%d, want 1100 220 55 1", prompt, completion, thinking, filtered)
	}

	// Why: in-memory token buffers are package globals not cleared by ResetForTest; drain the
	// un-flushed delta so it can't leak into subsequent tests.
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("final FlushTokenUsage: %v", err)
	}
}

// TestGetDailyTokenUsage_FreshCache pins the hot-cache path: when usageCache already holds
// today's totals, GetDailyTokenUsage must return them without touching the DB or in-memory
// buffers.
func TestGetDailyTokenUsage_FreshCache(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "daily-freshcache@example.com" // unique: in-memory token buffers are not reset between tests
	today := time.Now().Format("2006-01-02")

	usageCacheMu.Lock()
	usageCache[email] = &tokenUsageCacheData{
		Date:            today,
		DailyPrompt:     42,
		DailyCompletion: 7,
		DailyThinking:   3,
		DailyFiltered:   1,
	}
	usageCacheMu.Unlock()

	prompt, completion, thinking, filtered, err := GetDailyTokenUsage(ctx, email)
	if err != nil {
		t.Fatalf("GetDailyTokenUsage: %v", err)
	}
	if prompt != 42 || completion != 7 || thinking != 3 || filtered != 1 {
		t.Errorf("got prompt=%d completion=%d thinking=%d filtered=%d, want 42 7 3 1", prompt, completion, thinking, filtered)
	}
}

// TestGetMonthlyTokenUsage_DBAndInMemoryMerge pins the DB-plus-buffered-delta merge path
// for the monthly aggregate.
func TestGetMonthlyTokenUsage_DBAndInMemoryMerge(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "monthly-merge@example.com" // unique: in-memory token buffers are not reset between tests

	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 1000, 200, 50, 0)
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("FlushTokenUsage: %v", err)
	}
	IncrementFilteredCount(email)
	_ = AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 100, 20, 5, 0) // in-memory only

	prompt, completion, thinking, filtered, err := GetMonthlyTokenUsage(ctx, email)
	if err != nil {
		t.Fatalf("GetMonthlyTokenUsage: %v", err)
	}
	if prompt != 1100 || completion != 220 || thinking != 55 || filtered != 1 {
		t.Errorf("got prompt=%d completion=%d thinking=%d filtered=%d, want 1100 220 55 1", prompt, completion, thinking, filtered)
	}

	// Why: in-memory token buffers are package globals not cleared by ResetForTest; drain the
	// un-flushed delta so it can't leak into subsequent tests.
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("final FlushTokenUsage: %v", err)
	}
}

// TestGetMonthlyTokenUsage_FreshCache pins the hot-cache path: when usageCache already holds
// the current month's totals, GetMonthlyTokenUsage must return them without touching the DB
// or in-memory buffers.
func TestGetMonthlyTokenUsage_FreshCache(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "monthly-freshcache@example.com" // unique: in-memory token buffers are not reset between tests
	currentMonth := time.Now().Format("2006-01")

	usageCacheMu.Lock()
	usageCache[email] = &tokenUsageCacheData{
		Month:             currentMonth,
		MonthlyPrompt:     99,
		MonthlyCompletion: 11,
		MonthlyThinking:   6,
		MonthlyFiltered:   2,
	}
	usageCacheMu.Unlock()

	prompt, completion, thinking, filtered, err := GetMonthlyTokenUsage(ctx, email)
	if err != nil {
		t.Fatalf("GetMonthlyTokenUsage: %v", err)
	}
	if prompt != 99 || completion != 11 || thinking != 6 || filtered != 2 {
		t.Errorf("got prompt=%d completion=%d thinking=%d filtered=%d, want 99 11 6 2", prompt, completion, thinking, filtered)
	}
}
