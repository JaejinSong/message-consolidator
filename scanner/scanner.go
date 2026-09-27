package scanner

import (
	"context"
	"fmt"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/internal/primes"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"strings"
	"time"

	"message-consolidator/ai"

	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
	"sync"
)

var inFlightMessages sync.Map

// Why: Prime-second pool near 3600s for loops that do not require sub-minute cadence.
var hourPrimePool = []time.Duration{
	3557 * time.Second,
	3571 * time.Second,
	3593 * time.Second,
	3607 * time.Second,
	3613 * time.Second,
}

var cfg *config.Config

// scanDeps groups every service/client the scanner loops depend on.
// Why: one injection point (set once in Init, before StartBackgroundScanner)
// instead of scattered package globals, so tests swap deps fields in isolation.
type scanDeps struct {
	gClient         *ai.GeminiClient
	completionSvc   *services.CompletionService
	tasksSvc        *services.TasksService
	filterSvc       *ai.GeminiLiteFilter
	roomLockSvc     *services.RoomLockService
	slackClient     *channels.SlackClient
	reminderSvc     reminderDispatcher
	exclusionSvc    exclusionDispatcher
	pastEventSvc    pastEventDispatcher
	digestSvc       digestDispatcher
	weeklyReportSvc weeklyReportDispatcher
}

var deps scanDeps

func StartBackgroundScanner(ctx context.Context) {
	logger.Infof("[SCAN] background scanner started (second-cadence=%v hour-cadence=%v)", primes.Seconds, hourPrimePool)

	var wg sync.WaitGroup

	loops := []*primeLoop{
		{name: "gmail", traceName: "/Background-Gmail-Scan", runFn: runGmailForAllUsers},
		{name: "whatsapp", traceName: "/Background-WhatsApp-Scan", runFn: runWhatsAppForAllUsers},
		{name: "whatsapp-replay", traceName: "/Background-WhatsApp-Replay", runFn: runWhatsAppReplay, pool: waReplayPool},
		{name: "telegram", traceName: "/Background-Telegram-Scan", runFn: runTelegramForAllUsers},
		{name: "slack", traceName: "/Background-Slack-Scan", runFn: runSlackForAllUsers},
		{name: "line", traceName: "/Background-LINE-Scan", runFn: runLineForAllUsers},
		{name: "archive-old-tasks", traceName: "/Background-Tasks-Archive", runFn: runArchiveOldTasks, pool: hourPrimePool},
		{name: "flush-token-usage", traceName: "/Background-Infra-FlushTokenUsage", runFn: runFlushTokenUsage, pool: hourPrimePool},
		{name: "log-db-stats", traceName: "/Background-Infra-LogDBStats", runFn: runLogDBStats, pool: hourPrimePool},
		{name: "session-cleanup", traceName: "/Background-Infra-SessionCleanup", runFn: runSessionCleanup, pool: hourPrimePool},
		{name: "sweep-slack-threads", traceName: "/Background-Slack-SweepThreads", runFn: runSlackSweep},
		{name: "deadline-reminder", traceName: "/Background-Tasks-DeadlineReminder", runFn: runDeadlineReminder},
		{name: "stalled-reconfirm", traceName: "/Background-Tasks-StalledReconfirm", runFn: runStalledReconfirm, pool: hourPrimePool},
		{name: "exclusion-candidate", traceName: "/Background-Tasks-ExclusionCandidate", runFn: runExclusionCandidate, pool: hourPrimePool},
		{name: "past-event-candidate", traceName: "/Background-Tasks-PastEventCandidate", runFn: runPastEventCandidate, pool: hourPrimePool},
		{name: "precision-observer", traceName: "/Background-Tasks-PrecisionObserver", runFn: runPrecisionObserver, pool: hourPrimePool},
		{name: "excluded-digest", traceName: "/Background-Tasks-ExcludedDigest", runFn: runExcludedDigest, pool: hourPrimePool},
		{name: "daily-digest", traceName: "/Background-Reports-DailyDigest", runFn: runDailyDigest, pool: hourPrimePool},
		{name: "weekly-report", traceName: "/Background-Reports-WeeklyReport", runFn: runWeeklyReport, pool: hourPrimePool},
	}
	for _, l := range loops {
		first := l.pickNext()
		logger.Infof("[SCAN] %s loop start interval=%s", l.name, first)
		wg.Add(1)
		go l.start(ctx, &wg, first)
	}

	<-ctx.Done()
	logger.Infof("[SCAN] shutdown signal received; waiting for in-flight tasks...")

	//Why: Generous timeout allows AI-intensive scans and translations (15-20s) to complete before termination.
	done := make(chan struct{})
	go func() {
		defer safego.Recover("scanner-wg-sentinel")
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Infof("[SCAN] all background tasks finished gracefully.")
	case <-time.After(30 * time.Second):
		logger.Warnf("[SCAN] timeout waiting for background tasks; forcing exit.")
	}
}

func RunAllScans(ctx context.Context, wg *sync.WaitGroup) {
	// Why: trace.Start creates a new background TX. StartWithContext only renames an
	// existing trace ctx and silently skips when none exists (Trace.go:179-205) — the
	// scheduler tick passes a plain context.Background() so we need real Start semantics.
	// Name prefixed with `/` so urlutil.NewURL parses it as Path (otherwise the WhaTap
	// Transaction column shows blank because the name lands in Host instead).
	traceCtx, _ := trace.Start(ctx, "/Background-Scanner-RunAll")
	defer func() { _ = trace.End(traceCtx, nil) }()

	users, err := store.GetAllUsers(traceCtx)
	if err != nil {
		logger.Errorf("[SCAN] failed to get users: %v", err)
		return
	}

	scanUsersSourcesParallel(traceCtx, users, wg)
	performSlackScan(traceCtx, users, wg)
	finalizeScanCycle(traceCtx, users)
}

func scanUsersSourcesParallel(ctx context.Context, users []store.User, wg *sync.WaitGroup) {
	var eg errgroup.Group
	eg.SetLimit(5) // MaxConcurrentScans

	for _, user := range users {
		u := user
		eg.Go(func() error {
			aliases, _ := store.GetUserAliases(ctx, u.ID)
			scanAllSources(ctx, u, aliases, wg)
			return nil
		})
	}
	_ = eg.Wait()
}

func performSlackScan(ctx context.Context, users []store.User, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	sCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		defer safego.Recover("scan-slack")
		scanSlack(sCtx, users, wg)
	}()
}

func finalizeScanCycle(ctx context.Context, users []store.User) {
	for _, u := range users {
		store.PersistAllScanMetadata(ctx, u.Email)
	}

	_ = store.ArchiveOldTasks(ctx)
	store.FlushTokenUsageIfNeeded(ctx)
	store.LogDBStats()
}

func runSlackForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		logger.Errorf("[SCAN] failed to get users for slack scan: %v", err)
		return
	}
	scanCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	defer safego.Recover("scan-slack")
	scanSlack(scanCtx, users, wg)
}

func runArchiveOldTasks(ctx context.Context, _ *sync.WaitGroup) {
	_ = store.ArchiveOldTasks(ctx)
}

func runFlushTokenUsage(ctx context.Context, _ *sync.WaitGroup) {
	store.FlushTokenUsageIfNeeded(ctx)
}

func runLogDBStats(_ context.Context, _ *sync.WaitGroup) {
	store.LogDBStats()
}

func runSessionCleanup(ctx context.Context, _ *sync.WaitGroup) {
	if err := store.DeleteExpiredSessions(ctx); err != nil {
		logger.Warnf("[SCAN] session cleanup failed: %v", err)
	}
}

func runSlackSweep(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	sweepSlackThreads(ctx, wg)
}

func scanAllSources(parentCtx context.Context, user store.User, aliases []string, wg *sync.WaitGroup) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)
	ctx, cancel := context.WithTimeout(parentCtx, 45*time.Second)
	defer cancel()

	effAl := services.GetEffectiveAliases(user, aliases)
	_ = scanUserChannels(ctx, user.Email, effAl, wg)
	store.PersistAllScanMetadata(ctx, user.Email)
}
func scanUserChannels(ctx context.Context, email string, effAl []string, wg *sync.WaitGroup) error {
	var eg errgroup.Group
	if store.HasGmailToken(email) {
		eg.Go(func() error {
			return performGmailScan(ctx, email, wg)
		})
	}

	// Why: resolve once for both goroutines below — a failed lookup must not deref
	// a nil *User inside an errgroup goroutine (no safego.Recover crosses goroutines).
	user, ok := resolveScanUser(ctx, email, store.GetOrCreateUser)
	if !ok {
		logger.Warnf("[SCAN] skipping WhatsApp/Telegram for %s: user lookup failed", email)
		return eg.Wait()
	}

	eg.Go(func() error {
		scanWhatsApp(ctx, *user, effAl, "Korean", wg)
		return nil
	})

	eg.Go(func() error {
		scanTelegram(ctx, *user, effAl, "Korean", wg)
		return nil
	})
	return eg.Wait()
}

// resolveScanUser resolves the scanning user via lookup, guarding against a nil
// result or error so callers can skip per-user work instead of dereferencing nil.
func resolveScanUser(ctx context.Context, email string, lookup func(context.Context, string, string, string) (*store.User, error)) (*store.User, bool) {
	user, err := lookup(ctx, email, "", "")
	if err != nil || user == nil {
		logger.Warnf("[SCAN] failed to resolve user %s: %v", email, err)
		return nil, false
	}
	return user, true
}

func performGmailScan(ctx context.Context, email string, wg *sync.WaitGroup) error {
	// Why: onThreadActivity used to call ReleaseInFlight with a "gmail-%s-%s"
	// SourceTS key, but the dedupe map below is keyed "gmail-%s-%d" by MessageID —
	// the two never matched, so this call had no effect. Removed rather than fixed
	// because dedupe lifetime is now scoped to this call (see claimInFlight below).
	onThreadActivity := func(msg store.ConsolidatedMessage) bool {
		if deps.completionSvc == nil {
			return false
		}
		handled, _ := deps.completionSvc.ProcessPotentialCompletion(ctx, msg)
		return handled
	}
	ids := channels.ScanGmail(ctx, email, "Korean", cfg, deps.gClient, deps.filterSvc, onThreadActivity)

	// Why: previously these keys were never released (mismatched ReleaseInFlight key
	// format), leaking unbounded map entries and permanently skipping any repeated
	// MessageID. Scope the claim to this dispatch only, releasing right after
	// triggerAsyncTranslation is invoked so overlapping scans still dedupe.
	filteredIDs, release := claimInFlight(email, ids)
	defer release()

	triggerAsyncTranslation(ctx, email, filteredIDs, wg)
	return nil
}

// claimInFlight atomically reserves the given gmail message IDs for this dispatch,
// skipping any already claimed by a concurrent scan. The returned release func
// must be called once the caller is done dispatching, to avoid leaking entries.
func claimInFlight(email string, ids []store.MessageID) (claimed []store.MessageID, release func()) {
	for _, id := range ids {
		idStr := fmt.Sprintf("gmail-%s-%d", email, id)
		if _, loaded := inFlightMessages.LoadOrStore(idStr, true); loaded {
			logger.Debugf("[SCAN] gmail: message %s already in-flight, skipping.", idStr)
			continue
		}
		claimed = append(claimed, id)
	}
	release = func() {
		for _, id := range claimed {
			inFlightMessages.Delete(fmt.Sprintf("gmail-%s-%d", email, id))
		}
	}
	return claimed, release
}

func Scan(email string, lang string, wg *sync.WaitGroup) {
	traceCtx, _ := trace.Start(context.Background(), "/Scanner-Manual")
	defer func() { _ = trace.End(traceCtx, nil) }()

	user, err := store.GetOrCreateUser(traceCtx, email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] failed to get user %s: %v", email, err)
		return
	}

	ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
	defer cancel()

	effAl := services.GetEffectiveAliases(*user, func() []string {
		a, _ := store.GetUserAliases(traceCtx, user.ID)
		return a
	}())
	runManualScans(ctx, user, effAl, lang, wg)

	store.PersistAllScanMetadata(ctx, user.Email)
}

func runManualScans(ctx context.Context, user *store.User, effAl []string, lang string, wg *sync.WaitGroup) {
	if store.HasGmailToken(user.Email) {
		if err := performGmailScan(ctx, user.Email, wg); err != nil {
			logger.Warnf("[SCAN] gmail: scan failed for %s: %v", user.Email, err)
		}
	}
	scanSlack(ctx, []store.User{*user}, wg)
	scanWhatsApp(ctx, *user, effAl, lang, wg)
}

// Why: Provides strict matching for short aliases (like '나', 'me') to prevent false positives in common sentences,
// while allowing flexible substring matching for longer, unique names.
func isAliasMatched(text, sender, alias string) bool {
	lowerAlias := strings.ToLower(strings.TrimSpace(alias))
	if lowerAlias == "" {
		return false
	}
	aliasLen := len([]rune(lowerAlias))

	if sender != "" {
		lowerSender := strings.ToLower(sender)
		if lowerSender == lowerAlias || (aliasLen > 1 && strings.Contains(lowerSender, lowerAlias)) {
			return true
		}
	}

	if text == "" {
		return false
	}
	lowerText := strings.ToLower(text)
	if aliasLen > 2 {
		return strings.Contains(lowerText, lowerAlias)
	}
	// Why: Short aliases are highly susceptible to false positives (e.g., '나' inside '지나가다').
	// We tokenize the text and check for exact matches or Korean particle postfixes (e.g., '나는', '나를').
	for _, w := range strings.Fields(lowerText) {
		if w == lowerAlias || (strings.HasPrefix(w, lowerAlias) && len([]rune(w)) <= aliasLen+2) {
			return true
		}
	}
	return false
}

func triggerAsyncTranslation(ctx context.Context, email string, ids []store.MessageID, wg *sync.WaitGroup) {
	if deps.tasksSvc == nil || len(ids) == 0 {
		return
	}
	// Why: Asynchronously triggers pre-calculated translation, tracked via WaitGroup to ensure completion during graceful shutdown.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer safego.Recover("trigger-async-translation")
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		_, _ = deps.tasksSvc.ProcessBatchTranslation(tCtx, email, ids, "ko")
	}()
}

// TriggerWeeklyReport dispatches a one-off weekly report to the given recipient, bypassing day/hour checks.
func TriggerWeeklyReport(ctx context.Context, recipient string) error {
	if deps.weeklyReportSvc == nil {
		return fmt.Errorf("weekly report service not initialized")
	}
	return deps.weeklyReportSvc.DispatchTo(ctx, recipient)
}
