package scanner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"message-consolidator/store"
	"message-consolidator/types"
)

// TestTelegramAdapter_Enrich delegates to EnrichTelegramMessage; verify non-nil result.
func TestTelegramAdapter_Enrich(t *testing.T) {
	t.Parallel()
	roomKey := "tg_user_123"
	ts := time.Unix(1700000000, 0)

	got, err := (telegramAdapter{}).Enrich(roomKey, "some text", ts)
	if err != nil {
		t.Fatalf("telegramAdapter.Enrich() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("telegramAdapter.Enrich() returned nil")
	}
	if got.SourceChannel != "telegram" {
		t.Errorf("SourceChannel = %q, want %q", got.SourceChannel, "telegram")
	}
	if got.RawContent != "some text" {
		t.Errorf("RawContent = %q, want %q", got.RawContent, "some text")
	}
}

// TestTelegramAdapter_GetGroupName calls the DefaultTelegramManager (no registered users → empty string).
func TestTelegramAdapter_GetGroupName(t *testing.T) {
	// Why: DefaultTelegramManager has no registered user "none@x"; returns "" without panic.
	got := (telegramAdapter{}).GetGroupName("none@x", "tg_chat_1234")
	// We only assert no panic; return value depends on manager state.
	_ = got
}

// TestWhatsAppAdapter_GetGroupName mirrors the telegram case.
func TestWhatsAppAdapter_GetGroupName(t *testing.T) {
	got := (whatsAppAdapter{}).GetGroupName("none@x", "12345@g.us")
	_ = got
}

// TestPrepareSlackUserAliases_WithDB verifies it handles a real DB user.
func TestPrepareSlackUserAliases_WithDB(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	u, _ := store.GetOrCreateUser(ctx, "alias-test@example.com", "Alias User", "")
	if u == nil {
		t.Fatal("GetOrCreateUser returned nil")
	}
	users := []store.User{*u}
	result := prepareSlackUserAliases(ctx, users)
	if _, ok := result[u.Email]; !ok {
		t.Errorf("prepareSlackUserAliases missing email %q", u.Email)
	}
}

// TestPrepareSlackUserAliases_Empty verifies no panic on empty users.
func TestPrepareSlackUserAliases_Empty(t *testing.T) {
	result := prepareSlackUserAliases(context.Background(), nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

// TestProcessSlackCandidates_EmptyMap verifies the for-range over empty map does not panic.
func TestProcessSlackCandidates_EmptyMap(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	deps.gClient = nil

	wg := &sync.WaitGroup{}
	processSlackCandidates(context.Background(), nil, nil, map[string][]types.RawMessage{}, wg)
	wg.Wait()
}

// TestBuildSlackLinkAndRegisterThread_WithDB covers the store.RegisterTargetedSlackThread path.
func TestBuildSlackLinkAndRegisterThread_WithDB(t *testing.T) {
	initTestDB(t)

	m := types.RawMessage{ChannelID: "C1", ID: "1700000100.000000", ReplyToID: ""}
	email := "thread-reg@example.com"

	got := buildSlackLinkAndRegisterThread(context.Background(), m, email)
	if !strings.Contains(got, "C1") {
		t.Errorf("link does not contain channel ID: %q", got)
	}
	if !strings.Contains(got, "1700000100000000") {
		t.Errorf("link does not contain message ID digits: %q", got)
	}
}

// TestBuildSlackLinkAndRegisterThread_Reply_WithDB covers the reply (ReplyToID != "") path.
func TestBuildSlackLinkAndRegisterThread_Reply_WithDB(t *testing.T) {
	initTestDB(t)

	m := types.RawMessage{ChannelID: "C2", ID: "1700000200.000000", ReplyToID: "1700000100.000000"}
	email := "thread-reply@example.com"

	got := buildSlackLinkAndRegisterThread(context.Background(), m, email)
	if !strings.Contains(got, "thread_ts=1700000100.000000") {
		t.Errorf("link missing thread_ts param: %q", got)
	}
}

// TestRunManualScans_NilCfgNoSlackToken exercises the path where scanSlack returns early
// (no slack token) and scanWhatsApp + scanTelegram pop empty buffers.
func TestRunManualScans_NilCfgNoSlackToken(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	cfg = nil // scanSlack guard: cfg == nil → return

	user := &store.User{Email: "manual@example.com", Name: "Manual"}
	wg := &sync.WaitGroup{}
	runManualScans(context.Background(), user, nil, "Korean", wg)
	wg.Wait()
}

// TestScanUserChannels_WithDB verifies the function runs without panic when no Gmail token exists.
func TestScanUserChannels_WithDB(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	cfg = nil // no slack token

	ctx := context.Background()
	_, _ = store.GetOrCreateUser(ctx, "suc@example.com", "SCU", "")

	wg := &sync.WaitGroup{}
	_ = scanUserChannels(ctx, "suc@example.com", nil, wg)
	wg.Wait()
}
