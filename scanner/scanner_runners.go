package scanner

import (
	"context"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// userBundle pairs a user with their effective alias set, computed once per scan cycle.
type userBundle struct {
	user    store.User
	aliases []string
}

func loadUsersForScan(ctx context.Context) []userBundle {
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		logger.Errorf("[SCAN] failed to get users: %v", err)
		return nil
	}
	out := make([]userBundle, 0, len(users))
	for _, u := range users {
		al, _ := store.GetUserAliases(ctx, u.ID)
		out = append(out, userBundle{user: u, aliases: services.GetEffectiveAliases(u, al)})
	}
	return out
}

// Why: Gmail backlog recovery needs headroom — after a cursor hold (fetch/analyze
// failure) a cycle re-walks unmarked messages (per-message Get + LiteFilter + batch
// Analyze). 45s could be consumed by Gets alone, marking nothing and livelocking the
// backlog. Normal cycles finish in seconds, so the longer ceiling is dormant. Prime.
const gmailScanTimeout = 293 * time.Second

// Why: shared per-user window for whatsapp/telegram loops. Not prime (45 = 3*3*5);
// left as-is because it is an established production value and changing it is a
// behavior change, not a refactor.
const perUserScanTimeout = 45 * time.Second

// forEachUserBundle runs fn for every user that passes gate, bounded by an errgroup
// limit of 5 and a per-user timeout, then persists scan metadata for that user.
// Why: gmail/whatsapp/telegram "for all users" loops shared this exact shape
// (load bundles, gate, per-user timeout, recover, persist) before this extraction.
func forEachUserBundle(ctx context.Context, label string, timeout time.Duration, gate func(store.User) bool, fn func(scanCtx context.Context, b userBundle)) {
	forEachUserBundleWith(ctx, label, timeout, loadUsersForScan, gate, fn)
}

// forEachUserBundleWith is forEachUserBundle with an injectable user loader.
// Why: lets tests exercise gate/fn dispatch without a live store.GetAllUsers DB call.
func forEachUserBundleWith(ctx context.Context, label string, timeout time.Duration, load func(context.Context) []userBundle, gate func(store.User) bool, fn func(scanCtx context.Context, b userBundle)) {
	bundles := load(ctx)
	if len(bundles) == 0 {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(5) // MaxConcurrentScans
	for _, b := range bundles {
		b := b
		if gate != nil && !gate(b.user) {
			continue
		}
		eg.Go(func() error {
			scanCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			defer safego.Recover("scan-" + label)
			fn(scanCtx, b)
			store.PersistAllScanMetadata(scanCtx, b.user.Email)
			return nil
		})
	}
	_ = eg.Wait()
}

func runGmailForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	forEachUserBundle(ctx, "gmail", gmailScanTimeout, func(u store.User) bool {
		return store.HasGmailToken(u.Email)
	}, func(scanCtx context.Context, b userBundle) {
		if err := performGmailScan(scanCtx, b.user.Email, wg); err != nil {
			logger.Warnf("[SCAN] gmail: scan failed for %s: %v", b.user.Email, err)
		}
	})
}

func runWhatsAppForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	forEachUserBundle(ctx, "whatsapp", perUserScanTimeout, nil, func(scanCtx context.Context, b userBundle) {
		scanWhatsApp(scanCtx, b.user, b.aliases, "Korean", wg)
	})
}

func runTelegramForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	forEachUserBundle(ctx, "telegram", perUserScanTimeout, nil, func(scanCtx context.Context, b userBundle) {
		scanTelegram(scanCtx, b.user, b.aliases, "Korean", wg)
	})
}
