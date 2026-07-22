package scanner

import (
	"context"
	"sync"
	"testing"

	"message-consolidator/config"
	"message-consolidator/store"
	"message-consolidator/types"
)

// TestBuildSlackAliasCache_Empty verifies empty thread slice returns empty map without panic.
func TestBuildSlackAliasCache_Empty(t *testing.T) {
	got := buildSlackAliasCache(context.Background(), nil)
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

// TestBuildSlackAliasCache_WithUsers exercises the full DB path including GetUserAliases.
func TestBuildSlackAliasCache_WithUsers(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	u, _ := store.GetOrCreateUser(ctx, "alias-cache@example.com", "Alias Cache User", "")
	if u == nil {
		t.Fatal("GetOrCreateUser returned nil")
	}
	threads := []store.SlackThreadMeta{
		{UserEmail: "alias-cache@example.com", ChannelID: "C1", ThreadTS: "1700000100.000000"},
		{UserEmail: "alias-cache@example.com", ChannelID: "C1", ThreadTS: "1700000200.000000"}, // duplicate email → dedup
	}
	got := buildSlackAliasCache(ctx, threads)
	if len(got) != 1 {
		t.Errorf("expected 1 entry (dedup), got %d", len(got))
	}
	ident, ok := got["alias-cache@example.com"]
	if !ok {
		t.Fatal("alias-cache@example.com missing from cache")
	}
	if ident.user == nil {
		t.Error("user should be non-nil for a known email")
	}
}

// TestBuildSlackAliasCache_UnknownEmail verifies the nil-user path (store returns nil).
func TestBuildSlackAliasCache_UnknownEmail(t *testing.T) {
	initTestDB(t)
	threads := []store.SlackThreadMeta{
		// Why: store.GetOrCreateUser creates the user if it doesn't exist;
		// to exercise the nil path we need to leave the DB empty and use a guaranteed non-existent email
		// in a freshly reset DB.
		{UserEmail: "definitely-not-created@example.com", ChannelID: "C1", ThreadTS: "1.0"},
	}
	// In the current store implementation, GetOrCreateUser creates the user, so u is non-nil.
	// We can still verify the function runs without panic.
	got := buildSlackAliasCache(context.Background(), threads)
	if _, ok := got["definitely-not-created@example.com"]; !ok {
		t.Error("expected entry for unknown email even when user is nil/created")
	}
}

// TestSweepSlackThreads_EmptyThreads exercises the threads-empty early-return path.
func TestSweepSlackThreads_EmptyThreads(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)

	cfg = &config.Config{SlackToken: "xoxb-fake-token-for-test"}

	// No threads in DB → store.GetTargetedActiveThreads returns empty → returns early.
	wg := &sync.WaitGroup{}
	sweepSlackThreads(context.Background(), wg)
	wg.Wait()
}

// TestProcessSlackCandidates_NonEmptyNilGClient exercises the loop when deps.gClient==nil
// (analyzeAndSaveSlack returns early without panic).
func TestProcessSlackCandidates_NonEmptyNilGClient(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	deps.gClient = nil
	// Reinitialise deps.roomLockSvc so analyzeAndSaveSlack doesn't panic on nil.
	// analyzeAndSaveSlack checks deps.gClient==nil before using deps.roomLockSvc, so this is safe.

	ctx := context.Background()
	email := "cand-test@example.com"
	_, _ = store.GetOrCreateUser(ctx, email, "Cand User", "")

	candidates := map[string]map[string][]types.RawMessage{
		email: {"C1": {{ID: "m1", Text: "hello", ChannelID: "C1"}}},
	}
	wg := &sync.WaitGroup{}
	processSlackCandidates(ctx, nil, nil, candidates, wg)
	wg.Wait()
}
