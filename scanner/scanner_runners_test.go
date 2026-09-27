package scanner

import (
	"context"
	"sync"
	"testing"
	"time"

	"message-consolidator/store"
)

func TestForEachUserBundleWith_SkipsGatedUsersAndRunsFnForGatedOnes(t *testing.T) {
	bundles := []userBundle{
		{user: store.User{Email: "allowed@example.com"}},
		{user: store.User{Email: "blocked@example.com"}},
		{user: store.User{Email: "also-allowed@example.com"}},
	}
	load := func(context.Context) []userBundle { return bundles }
	gate := func(u store.User) bool { return u.Email != "blocked@example.com" }

	var mu sync.Mutex
	var ran []string
	fn := func(scanCtx context.Context, b userBundle) {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, b.user.Email)
	}

	forEachUserBundleWith(context.Background(), "test", 5*time.Second, load, gate, fn)

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 {
		t.Fatalf("expected fn to run for 2 gated users, got %d: %v", len(ran), ran)
	}
	seen := map[string]bool{}
	for _, e := range ran {
		seen[e] = true
	}
	if !seen["allowed@example.com"] || !seen["also-allowed@example.com"] {
		t.Fatalf("expected fn to run for allowed users, got: %v", ran)
	}
	if seen["blocked@example.com"] {
		t.Fatalf("expected fn to skip blocked user, got: %v", ran)
	}
}

func TestForEachUserBundleWith_NoUsersIsNoop(t *testing.T) {
	load := func(context.Context) []userBundle { return nil }
	called := false
	fn := func(scanCtx context.Context, b userBundle) { called = true }

	forEachUserBundleWith(context.Background(), "test", 5*time.Second, load, nil, fn)

	if called {
		t.Fatalf("expected fn not to be called when load returns no bundles")
	}
}
