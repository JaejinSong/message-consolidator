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

	"message-consolidator/types"
	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
	"sync"
)

var (
	cfg           *config.Config
	completionSvc *services.CompletionService
	tasksSvc      *services.TasksService
	filterSvc     *ai.GeminiLiteFilter
	roomLockSvc   *services.RoomLockService
)

func Init(c *config.Config) {
	cfg = c
	roomLockSvc = services.NewRoomLockService()
	if cfg.GeminiAPIKey != "" {
		gClient, err := ai.NewGeminiClient(context.Background(), cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("[SCANNER] Failed to init GeminiClient: %v", err)
			return
		}
		completionSvc = services.NewCompletionService(gClient, &services.DefaultTaskStore{})
		transSvc := services.NewTranslationService(gClient)
		tasksSvc = services.NewTasksService(transSvc, gClient)
		filterSvc = ai.NewGeminiLiteFilter(gClient)
	}
}

func StartBackgroundScanner(ctx context.Context) {
	logger.Infof("Background scanner started (59s interval for anti-resonance)...")
	ticker := time.NewTicker(59 * time.Second)
	defer ticker.Stop()

	var wg sync.WaitGroup

	//Why: Triggers an immediate scan upon startup to ensure the dashboard is populated without waiting for the first ticker interval.
	RunAllScans(ctx, &wg)

	//Why: Starts the background archival process (Slow Sweeper) to periodically clean up outdated tasks and maintain database performance.
	wg.Add(1)
	go startSlowSweeper(ctx, &wg)

	for {
		select {
		case <-ctx.Done():
			logger.Infof("[SCANNER] Shutdown signal received. Waiting for in-flight tasks...")
			
			//Why: Uses a generous timeout to allow AI-intensive scans and translations (which can take 15-20s) to complete before termination.
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
			return
		case <-ticker.C:
			RunAllScans(ctx, &wg)
		}
	}
}

func RunAllScans(ctx context.Context, wg *sync.WaitGroup) {
	traceCtx, _ := trace.StartWithContext(ctx, "Background-RunAllScans")
	defer trace.End(traceCtx, nil)

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
		scanSlack(sCtx, users, wg)
	}()
}

func finalizeScanCycle(ctx context.Context, users []store.User) {
	for _, u := range users {
		store.PersistAllScanMetadata(u.Email)
	}

	_ = store.ArchiveOldTasks(ctx)
	store.FlushTokenUsageIfNeeded(ctx)
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

func scanAllSources(parentCtx context.Context, user store.User, aliases []string, wg *sync.WaitGroup) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)
	ctx, cancel := context.WithTimeout(parentCtx, 45*time.Second)
	defer cancel()

	effAl := getEffectiveAliases(user, aliases)
	_ = scanUserChannels(ctx, user.Email, effAl, wg)
	store.PersistAllScanMetadata(user.Email)
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
	return eg.Wait()
}

func performGmailScan(ctx context.Context, email string, wg *sync.WaitGroup) error {
	onThreadActivity := func(msg store.ConsolidatedMessage) {
		if completionSvc != nil {
			completionSvc.ProcessPotentialCompletion(context.Background(), msg)
		}
	}
	ids := channels.ScanGmail(ctx, email, "Korean", cfg, onThreadActivity)
	triggerAsyncTranslation(ctx, email, ids, wg)
	return nil
}


func Scan(email string, lang string, wg *sync.WaitGroup) {
	traceCtx, _ := trace.StartWithContext(context.Background(), "ManualScan")
	defer trace.End(traceCtx, nil)

	user, err := store.GetOrCreateUser(traceCtx, email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] Failed to get user %s: %v", email, err)
		return
	}
	
	ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
	defer cancel()

	effAl := getEffectiveAliases(*user, func() []string {
		a, _ := store.GetUserAliases(traceCtx, user.ID)
		return a
	}())
	runManualScans(ctx, user, effAl, lang, wg)
	
	store.PersistAllScanMetadata(user.Email)
}

func runManualScans(ctx context.Context, user *store.User, effAl []string, lang string, wg *sync.WaitGroup) {
	if store.HasGmailToken(user.Email) {
		performGmailScan(ctx, user.Email, wg)
	}
	scanSlack(ctx, []store.User{*user}, wg)
	scanWhatsApp(ctx, *user, effAl, lang, wg)
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

func triggerAsyncTranslation(ctx context.Context, email string, ids []int, wg *sync.WaitGroup) {
	if tasksSvc == nil || len(ids) == 0 {
		return
	}
	// Why: Asynchronously triggers pre-calculated translation, tracked via WaitGroup to ensure completion during graceful shutdown.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		_, _ = tasksSvc.ProcessBatchTranslation(tCtx, email, ids, "ko")
	}()
}

// BuildTaskParams holds cross-platform metadata required for message consolidation.
type BuildTaskParams struct {
	User           store.User
	Item           store.TodoItem
	Raw            types.RawMessage
	Source         string
	Room           string
	Link           string
	ThreadID       string
	SourceChannels []string
}

// BuildConsolidatedMessage creates a unified message entity from AI results and platform metadata.
func BuildConsolidatedMessage(p BuildTaskParams, aliases []string) store.ConsolidatedMessage {
	category := p.Item.Category
	if category == "" {
		category = string(types.CategoryTask)
	}

	return store.ConsolidatedMessage{
		UserEmail: p.User.Email, Source: p.Source, Room: p.Room,
		Task: p.Item.Task, Requester: p.Item.Requester,
		Assignee:   NormalizeAssignee(p.Item.Assignee, p.User, aliases),
		AssignedAt: p.Raw.Timestamp, Link: p.Link, SourceTS: p.Raw.ID,
		OriginalText: p.Raw.Text, Category: category, ThreadID: p.ThreadID,
		SourceChannels: p.SourceChannels,
	}
}

// NormalizeAssignee centralizes self-identification across languages ("나", "me") and user aliases.
func NormalizeAssignee(assignee string, user store.User, aliases []string) string {
	lowerAsg := strings.ToLower(strings.TrimSpace(assignee))
	if lowerAsg == "" {
		return services.AssigneeShared
	}

	// Why: Ensures "Me" or "나" results in the user's primary name for consistent dashboard auditing.
	selfIDs := []string{"나", "me", "__current_user__", "담당자", strings.ToLower(user.Name), strings.ToLower(user.Email)}
	for _, id := range selfIDs {
		if id != "" && lowerAsg == id {
			return getPreferredName(user)
		}
	}

	for _, alias := range aliases {
		if alias != "" && lowerAsg == strings.ToLower(alias) {
			return getPreferredName(user)
		}
	}
	return assignee
}

func getPreferredName(user store.User) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Email
}
