package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"fmt"
	"strings"
	"time"

	"message-consolidator/ai"

	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
	"sync"
)

var inFlightMessages sync.Map

var (
	cfg           *config.Config
	gClient       *ai.GeminiClient
	completionSvc *services.CompletionService
	tasksSvc      *services.TasksService
	filterSvc     *ai.GeminiLiteFilter
	roomLockSvc   *services.RoomLockService
	slackClient   *channels.SlackClient
)

func Init(c *config.Config) {
	cfg = c
	roomLockSvc = services.NewRoomLockService()
	if cfg.GeminiAPIKey != "" {
		gc, err := ai.NewGeminiClient(context.Background(), cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("[SCANNER] Failed to init GeminiClient: %v", err)
			return
		}
		gClient = gc
		transSvc := services.NewTranslationService(gClient)
		tasksSvc = services.NewTasksService(transSvc, gClient)
		completionSvc = services.NewCompletionService(gClient, &services.DefaultTaskStore{}, tasksSvc, store.GetDB())
		filterSvc = ai.NewGeminiLiteFilter(gClient)
	}
	if cfg.SlackToken != "" {
		slackClient = channels.NewSlackClient(cfg.SlackToken)
		reminderSvc = services.NewReminderService(slackClient, cfg.ReminderWindowsHours)
		if cfg.DailyDigestEnabled && cfg.DailyDigestRecipientEmail != "" {
			digestSvc = services.NewDailyDigestService(slackClient, cfg.DailyDigestRecipientEmail, cfg.DailyDigestListLimit, cfg.DailyDigestTimezone)
		}
	}
}

func StartBackgroundScanner(ctx context.Context) {
	logger.Infof("Background scanner started (per-loop random prime cadence from %v)...", primePool)

	var wg sync.WaitGroup

	loops := []*primeLoop{
		{name: "gmail", traceName: "/Background-ScanGmail", runFn: runGmailForAllUsers},
		{name: "whatsapp", traceName: "/Background-ScanWhatsApp", runFn: runWhatsAppForAllUsers},
		{name: "telegram", traceName: "/Background-ScanTelegram", runFn: runTelegramForAllUsers},
		{name: "slack", traceName: "/Background-ScanSlack", runFn: runSlackForAllUsers},
		{name: "archive-old-tasks", traceName: "/Background-ArchiveOldTasks", runFn: runArchiveOldTasks},
		{name: "flush-token-usage", traceName: "/Background-FlushTokenUsage", runFn: runFlushTokenUsage},
		{name: "log-db-stats", traceName: "/Background-LogDBStats", runFn: runLogDBStats},
		{name: "sweep-slack-threads", traceName: "/Background-SweepSlackThreads", runFn: runSlackSweep},
		{name: "deadline-reminder", traceName: "/Background-DeadlineReminder", runFn: runDeadlineReminder},
		{name: "daily-digest", traceName: "/Background-DailyDigest", runFn: runDailyDigest},
		{name: "weekly-report", traceName: "/Background-WeeklyReport", runFn: runWeeklyReport},
	}
	for _, l := range loops {
		first := pickPrime()
		logger.Infof("[SCANNER] %s loop start interval=%s", l.name, first)
		wg.Add(1)
		go l.start(ctx, &wg, first)
	}

	<-ctx.Done()
	logger.Infof("[SCANNER] Shutdown signal received. Waiting for in-flight tasks...")

	//Why: Generous timeout allows AI-intensive scans and translations (15-20s) to complete before termination.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Infof("[SCANNER] All background tasks finished gracefully.")
	case <-time.After(30 * time.Second):
		logger.Warnf("[SCANNER] Timeout waiting for background tasks to finish. Forcing exit.")
	}
}

func RunAllScans(ctx context.Context, wg *sync.WaitGroup) {
	// Why: trace.Start creates a new background TX. StartWithContext only renames an
	// existing trace ctx and silently skips when none exists (Trace.go:179-205) — the
	// scheduler tick passes a plain context.Background() so we need real Start semantics.
	// Name prefixed with `/` so urlutil.NewURL parses it as Path (otherwise the WhaTap
	// Transaction column shows blank because the name lands in Host instead).
	traceCtx, _ := trace.Start(ctx, "/Background-RunAllScans")
	defer func() { _ = trace.End(traceCtx, nil) }()

	users, err := store.GetAllUsers(traceCtx)
	if err != nil {
		logger.Errorf("Scanner Error: Failed to get users: %v", err)
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

// userBundle pairs a user with their effective alias set, computed once per scan cycle.
type userBundle struct {
	user    store.User
	aliases []string
}

func loadUsersForScan(ctx context.Context) []userBundle {
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		logger.Errorf("[SCANNER] Failed to get users: %v", err)
		return nil
	}
	out := make([]userBundle, 0, len(users))
	for _, u := range users {
		al, _ := store.GetUserAliases(ctx, u.ID)
		out = append(out, userBundle{user: u, aliases: services.GetEffectiveAliases(u, al)})
	}
	return out
}

func runGmailForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	bundles := loadUsersForScan(ctx)
	if len(bundles) == 0 {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(5)
	for _, b := range bundles {
		b := b
		if !store.HasGmailToken(b.user.Email) {
			continue
		}
		eg.Go(func() error {
			scanCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			defer safego.Recover("scan-gmail")
			_ = performGmailScan(scanCtx, b.user.Email, wg)
			store.PersistAllScanMetadata(scanCtx, b.user.Email)
			return nil
		})
	}
	_ = eg.Wait()
}

func runWhatsAppForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	bundles := loadUsersForScan(ctx)
	if len(bundles) == 0 {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(5)
	for _, b := range bundles {
		b := b
		eg.Go(func() error {
			scanCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			defer safego.Recover("scan-whatsapp")
			scanWhatsApp(scanCtx, b.user, b.aliases, "Korean", wg)
			store.PersistAllScanMetadata(scanCtx, b.user.Email)
			return nil
		})
	}
	_ = eg.Wait()
}

func runTelegramForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	bundles := loadUsersForScan(ctx)
	if len(bundles) == 0 {
		return
	}
	var eg errgroup.Group
	eg.SetLimit(5)
	for _, b := range bundles {
		b := b
		eg.Go(func() error {
			scanCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			defer safego.Recover("scan-telegram")
			scanTelegram(scanCtx, b.user, b.aliases, "Korean", wg)
			store.PersistAllScanMetadata(scanCtx, b.user.Email)
			return nil
		})
	}
	_ = eg.Wait()
}

func runSlackForAllUsers(ctx context.Context, wg *sync.WaitGroup) {
	if cfg == nil || cfg.SlackToken == "" {
		return
	}
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		logger.Errorf("[SCANNER] Failed to get users for Slack scan: %v", err)
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

	eg.Go(func() error {
		user, _ := store.GetOrCreateUser(ctx, email, "", "")
		scanWhatsApp(ctx, *user, effAl, "Korean", wg)
		return nil
	})

	eg.Go(func() error {
		user, _ := store.GetOrCreateUser(ctx, email, "", "")
		scanTelegram(ctx, *user, effAl, "Korean", wg)
		return nil
	})
	return eg.Wait()
}

func performGmailScan(ctx context.Context, email string, wg *sync.WaitGroup) error {
	onThreadActivity := func(msg store.ConsolidatedMessage) bool {
		if completionSvc != nil {
			idStr := fmt.Sprintf("gmail-%s-%s", msg.UserEmail, msg.SourceTS)
			handled, _ := completionSvc.ProcessPotentialCompletion(ctx, msg)
			if handled {
				ReleaseInFlight(idStr)
			}
			return handled
		}
		return false
	}
	ids := channels.ScanGmail(ctx, email, "Korean", cfg, gClient, filterSvc, onThreadActivity)

	var filteredIDs []store.MessageID
	for _, id := range ids {
		idStr := fmt.Sprintf("gmail-%s-%d", email, id)
		if _, loaded := inFlightMessages.LoadOrStore(idStr, true); loaded {
			logger.Infof("[SCANNER] Message %s already in-flight, skipping.", idStr)
			continue
		}
		filteredIDs = append(filteredIDs, id)
	}

	triggerAsyncTranslation(ctx, email, filteredIDs, wg)
	return nil
}

func ReleaseInFlight(id string) {
	inFlightMessages.Delete(id)
}


func Scan(email string, lang string, wg *sync.WaitGroup) {
	traceCtx, _ := trace.Start(context.Background(), "/ManualScan")
	defer func() { _ = trace.End(traceCtx, nil) }()

	user, err := store.GetOrCreateUser(traceCtx, email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] Failed to get user %s: %v", email, err)
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
			logger.Warnf("[SCAN] Gmail scan failed for %s: %v", user.Email, err)
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
	if tasksSvc == nil || len(ids) == 0 {
		return
	}
	// Why: Asynchronously triggers pre-calculated translation, tracked via WaitGroup to ensure completion during graceful shutdown.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer safego.Recover("trigger-async-translation")
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		_, _ = tasksSvc.ProcessBatchTranslation(tCtx, email, ids, "ko")
	}()
}

// Why: WeeklyReportService needs reportsSvc which is built post-Init in main.go's initAIServices.
func WireWeeklyReport(reportsSvc *services.ReportsService) {
	if cfg == nil || !cfg.WeeklyReportEnabled || reportsSvc == nil || slackClient == nil {
		return
	}
	if cfg.WeeklyReportRecipientEmail == "" {
		logger.Warnf("[WEEKLY] recipient email not set")
		return
	}
	notion := services.NewNotionExporter(cfg.NotionToken, cfg.NotionReportPageID)
	if !notion.Enabled() {
		logger.Warnf("[WEEKLY] notion not configured")
		return
	}
	weeklyReportSvc = services.NewWeeklyReportService(slackClient, reportsSvc, notion, services.WeeklyReportConfig{
		RecipientEmail: cfg.WeeklyReportRecipientEmail,
		Hour:           cfg.WeeklyReportHour,
		Timezone:       cfg.WeeklyReportTimezone,
		Language:       cfg.WeeklyReportLang,
	})
}

