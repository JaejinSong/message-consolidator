package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/config"
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

var (
	cfg           *config.Config
	completionSvc *services.CompletionService
)

func Init(c *config.Config) {
	cfg = c
	if cfg.GeminiAPIKey != "" {
		gClient, err := ai.NewGeminiClient(context.Background(), cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("[SCANNER] Failed to init GeminiClient for completion service: %v", err)
		} else {
			completionSvc = services.NewCompletionService(gClient, &services.DefaultTaskStore{})
		}
	}
}

func StartBackgroundScanner(ctx context.Context) {
	logger.Infof("Background scanner started (59s interval for anti-resonance)...")
	ticker := time.NewTicker(59 * time.Second)
	defer ticker.Stop()

	var wg sync.WaitGroup

	// 초기 실행
	RunAllScans(&wg)

	// Sweeper 시작 (context와 WaitGroup 전달)
	wg.Add(1)
	go startSlowSweeper(ctx, &wg)

	for {
		select {
		case <-ctx.Done():
			logger.Infof("[SCANNER] Shutdown signal received. Waiting for in-flight tasks...")
			wg.Wait()
			logger.Infof("[SCANNER] All background tasks finished. Exit.")
			return
		case <-ticker.C:
			RunAllScans(&wg)
		}
	}
}

func RunAllScans(wg *sync.WaitGroup) {
	traceCtx, _ := trace.StartWithContext(context.Background(), "Background-RunAllScans")
	defer trace.End(traceCtx, nil)

	users, err := store.GetAllUsers()
	if err != nil {
		logger.Errorf("Scanner Error: Failed to get users: %v", err)
		return
	}

	var eg errgroup.Group
	// Why: Limit concurrent user scans to prevent connection pool exhaustion and strict API rate limits.
	const maxConcurrentScans = 5
	eg.SetLimit(maxConcurrentScans)

	for _, user := range users {
		aliases, _ := store.GetUserAliases(user.ID)

		u, al := user, aliases
		eg.Go(func() error {
			scanAllSources(u, al)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		logger.Errorf("Scanner Error: One or more user scans failed: %v", err)
	}

	// Why: Slack rate limits (HTTP 429) are extremely strict. Instead of scanning per user,
	// we iterate through all channels the bot has joined exactly once and map tasks to users in memory.
	if cfg != nil && cfg.SlackToken != "" {
		ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanSlack(ctx, users)
			cancel()
		}()

		for _, u := range users {
			store.PersistAllScanMetadata(u.Email)
		}
	}

	if err := store.ArchiveOldTasks(); err != nil {
		logger.Errorf("Scanner Error: Failed to archive old tasks: %v", err)
	}

	// Why: Piggyback on the background scanner loop to flush gamification data, reducing the need for a separate ticker.
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("Scanner Error: Failed to flush gamification data: %v", err)
	}

	// Why: Flush buffered token usage periodically so the database can enter scale-to-zero (sleep) mode without losing billing data.
	store.FlushTokenUsageIfNeeded()

	// Why: Log connection pool stats to verify all connections are returned, which is critical for Turso's scale-to-zero capability.
	store.LogDBStats()
}

// Why: Automatically include the user's name and email prefix as default aliases to prevent missed mentions,
// requiring less manual configuration from the user.
func getEffectiveAliases(user store.User, aliases []string) []string {
	unique := make(map[string]bool)
	for _, a := range aliases {
		if a != "" {
			unique[a] = true
		}
	}
	if user.Name != "" {
		unique[user.Name] = true
	}
	if prefix, _, found := strings.Cut(user.Email, "@"); found && prefix != "" {
		unique[prefix] = true
	}

	var result []string
	for a := range unique {
		result = append(result, a)
	}
	return result
}

func scanAllSources(user store.User, aliases []string) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)

	// Why: We use a standard context with timeout rather than errgroup.WithContext.
	// If one channel (e.g., Gmail) fails, it should not cancel the scanning of others (e.g., WhatsApp).
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var eg errgroup.Group
	effectiveAliases := getEffectiveAliases(user, aliases)

	if store.HasGmailToken(user.Email) {
		eg.Go(func() error {
			logger.Debugf("[SCAN] Starting Gmail scan for %s", user.Email)
			onSent := func(msg store.ConsolidatedMessage) {
				if completionSvc != nil {
					completionSvc.ProcessPotentialCompletion(context.Background(), msg)
				}
			}
			channels.ScanGmail(ctx, user.Email, "Korean", cfg, onSent)
			return nil
		})
	}

	eg.Go(func() error {
		logger.Debugf("[SCAN] Starting WhatsApp scan for %s", user.Email)
		scanWhatsApp(ctx, user, effectiveAliases, "Korean")
		return nil
	})

	eg.Wait()

	store.PersistAllScanMetadata(user.Email)
}

func Scan(email string, lang string) {
	traceCtx, _ := trace.StartWithContext(context.Background(), "ManualScan")
	defer trace.End(traceCtx, nil)

	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] Failed to get user %s: %v", email, err)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)
	effectiveAliases := getEffectiveAliases(*user, aliases)

	// Why: Bounded timeout prevents manual scans from hanging the HTTP handler indefinitely.
	ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
	defer cancel()

	if store.HasGmailToken(email) {
		onSent := func(msg store.ConsolidatedMessage) {
			if completionSvc != nil {
				completionSvc.ProcessPotentialCompletion(context.Background(), msg)
			}
		}
		channels.ScanGmail(ctx, email, lang, cfg, onSent)
	}

	scanSlack(ctx, []store.User{*user})

	scanWhatsApp(ctx, *user, effectiveAliases, lang)

	store.PersistAllScanMetadata(user.Email)

	// Why: Piggyback gamification flush on manual scans for immediate visual feedback in the UI.
	_ = services.FlushGamificationData()
}

// Why: Provides strict matching for short aliases (like '나', 'me') to prevent false positives in common sentences,
// while allowing flexible substring matching for longer, unique names.
func IsAliasMatched(text, sender, alias string) bool {
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

	if text != "" {
		lowerText := strings.ToLower(text)
		if aliasLen <= 2 {
			// Why: Short aliases are highly susceptible to false positives (e.g., '나' inside '지나가다').
			// We tokenize the text and check for exact matches or Korean particle postfixes (e.g., '나는', '나를').
			words := strings.Fields(lowerText)
			for _, w := range words {
				if w == lowerAlias || (strings.HasPrefix(w, lowerAlias) && len([]rune(w)) <= aliasLen+2) {
					return true
				}
			}
		} else {
			if strings.Contains(lowerText, lowerAlias) {
				return true
			}
		}
	}

	return false
}
