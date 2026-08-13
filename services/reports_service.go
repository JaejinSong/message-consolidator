package services

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"time"

	"github.com/whatap/go-api/trace"
)

// ReportSummarizer defines the strategy for generating report summaries from logs.
// reportID enables per-report token cost attribution.
type ReportSummarizer interface {
	Generate(ctx context.Context, email, logs, window string, reportID store.ReportID) (string, error)
}

// ReportConfig encapsulates configuration parameters for the report service.
type ReportConfig struct {
	CutoffSize int
}

// Why: ~16K tokens at 4 chars/token — well within Gemini 3 Flash's 1M context; covers 250+ task
// heavy weeks after done-task evidence removal. Tunable via REPORT_CUTOFF_SIZE env var.
const DefaultReportCutoffSize = 65000

// Why: bound JIT translation latency so async report goroutines and slow Gemini calls
// can't keep the request/parent context alive indefinitely. WithTimeout derives from
// the caller's deadline, so a tighter parent deadline still wins.
const defaultOnDemandTranslationTimeout = 30 * time.Second

// Log is a type alias for ConsolidatedMessage to satisfy technical requirements while maintaining consistency.
type Log = store.ConsolidatedMessage

// FlashSingleSummarizer implements ReportSummarizer using a single Gemini Flash model call.
type FlashSingleSummarizer struct {
	gemini *ai.GeminiClient
}

func NewFlashSingleSummarizer(gemini *ai.GeminiClient) *FlashSingleSummarizer {
	return &FlashSingleSummarizer{gemini: gemini}
}

// Generate implements the ReportSummarizer interface by calling the Gemini API for a single-pass summary.
func (s *FlashSingleSummarizer) Generate(ctx context.Context, email, logs, window string, reportID store.ReportID) (string, error) {
	return s.gemini.GenerateReportSummary(ctx, email, logs, window, reportID)
}

type ReportsService struct {
	summarizer     ReportSummarizer
	geminiClient   *ai.GeminiClient
	translationSvc *TranslationService
	config         ReportConfig
	isTest         bool
}

func NewReportsService(summarizer ReportSummarizer, geminiClient *ai.GeminiClient, trans *TranslationService, config ReportConfig) *ReportsService {
	return &ReportsService{
		summarizer:     summarizer,
		geminiClient:   geminiClient,
		translationSvc: trans,
		config:         config,
	}
}

// Why: Date-range cache only kicks in for unfiltered queries — source/done filters bypass the cached report by design.
func (s *ReportsService) findReusableReport(ctx context.Context, email, start, end string, source *string, done *bool) *store.Report {
	if source != nil || done != nil {
		return nil
	}
	existing, _ := store.GetReportByDateRange(ctx, email, start, end)
	if existing == nil {
		return nil
	}
	if existing.Status != store.ReportStatusProcessing && existing.Status != store.ReportStatusCompleted {
		return nil
	}
	if existing.Status == store.ReportStatusCompleted {
		existing.Translations, _ = store.GetReportTranslations(ctx, existing.ID)
	}
	return existing
}

// Why: Orchestrates the generation of an AI-powered work report.
func (s *ReportsService) GenerateReport(ctx context.Context, email, start, end, lang string, source *string, done *bool) (*store.Report, error) {
	// 1. Check for processing or existing
	// Note: We ignore cache for filtered reports as the date-based cache in GetReportByDate
	// currently doesn't account for source/status filters.
	if existing := s.findReusableReport(ctx, email, start, end, source, done); existing != nil {
		return existing, nil
	}

	// 2. Fetch and sanitize
	activity, stalled, err := s.fetchAndFilterMessages(ctx, email, start, end, source, done)
	if err != nil {
		return nil, err
	}
	// Why: Batch-resolve identities in one DB call by sanitizing combined slice, then split back by original lengths.
	actLen := len(activity)
	combined := append(append([]Log{}, activity...), stalled...)
	combined, _ = s.sanitizeMessages(ctx, email, combined) // Ignore error, self-healing
	activity = combined[:actLen]
	stalled = combined[actLen:]

	// 3. Create Placeholder
	report := &store.Report{
		UserEmail: email, StartDate: start, EndDate: end,
		Status: store.ReportStatusProcessing, Visualization: "{}", Translations: make(map[string]string),
	}
	id, err := store.SaveReport(ctx, report)
	if err != nil {
		return nil, err
	}
	report.ID = id

	// 4. Background Job
	if s.isTest {
		s.processAsyncReport(email, start, end, lang, report.ID, activity, stalled) //nolint:contextcheck // Identity resolution chain (Wave 2 I).
		// 💡 Sync update for test: Re-fetch report to ensure all fields (Status, Summary, Translations) are refreshed
		refreshed, err := store.GetReportByID(ctx, report.ID, email)
		if err == nil {
			*report = *refreshed
		}
	} else {
		go func() { //nolint:contextcheck // Identity resolution chain (Wave 2 I).
			defer safego.Recover("async-report")
			s.processAsyncReport(email, start, end, lang, report.ID, activity, stalled)
		}()
	}

	return report, nil
}

func (s *ReportsService) fetchAndFilterMessages(ctx context.Context, email, startDate, endDate string, source *string, done *bool) (activity []Log, stalled []Log, err error) {
	start, _ := time.Parse("2006-01-02", startDate)
	messages, err := store.GetMessagesForReport(ctx, email, start, source, done)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range messages {
		t := m.CreatedAt
		if m.UpdatedAt != nil {
			t = *m.UpdatedAt
		}
		ds := t.Format("2006-01-02")
		if ds >= startDate && ds <= endDate {
			activity = append(activity, m)
		}
	}

	// Why: Fetch stalled tasks predating the window separately so they appear in their own
	// section in the AI prompt — not counted in Activity, only used for Stalled Tasks rule.
	// Skip when caller requested done-only view — done tasks cannot be stalled.
	if done == nil || !*done {
		doneFalse := false
		threshold := store.GetStaleThresholdWorkingDays()
		// Why: zero time = no lower bound so tasks older than the threshold are fetched;
		// stale filter (WorkingDaysSince >= threshold) is applied in Go below.
		stalledMsgs, _ := store.GetMessagesForReport(ctx, email, time.Time{}, source, &doneFalse)
		for _, m := range stalledMsgs {
			// Skip tasks already captured in the activity window.
			if ds := m.CreatedAt.Format("2006-01-02"); ds >= startDate {
				continue
			}
			base := m.CreatedAt
			if !m.AssignedAt.IsZero() && m.AssignedAt.After(base) {
				base = m.AssignedAt
			}
			if store.WorkingDaysSince(base, time.Now()) >= threshold {
				stalled = append(stalled, m)
			}
		}
	}

	if len(activity) == 0 && len(stalled) == 0 {
		return nil, nil, fmt.Errorf("no messages found for %s ~ %s (source: %v, done: %v)", startDate, endDate, source, done)
	}
	return activity, stalled, nil
}

func (s *ReportsService) processAsyncReport(email, start, end, lang string, id store.ReportID, activity, stalled []Log) {
	// Why: trace.Start (not StartWithContext) creates a NEW trace context on a fresh
	// background ctx — StartWithContext silently skips when no parent trace ctx exists.
	// Name MUST start with `/` so urlutil.NewURL parses it as Path; without the slash
	// it becomes Host and the WhaTap Transaction column renders blank.
	ctx, _ := trace.Start(context.Background(), "/Reports-Generate")
	var err error
	defer func() { _ = trace.End(ctx, err) }()

	taskLogs, isTruncated := s.PrepareLogsForAI(email, activity, stalled)
	if isTruncated {
		logger.Warnf("[REPORTS] input logs truncated at cutoff (%d bytes): email=%s, total_logs=%d, report_id=%d",
			s.config.CutoffSize, email, len(activity)+len(stalled), id)
	}
	summary, err := s.summarizer.Generate(ctx, email, taskLogs, start+" ~ "+end, id)
	if err != nil {
		s.markFailed(ctx, email, id)
		return
	}
	vizJSON := s.getVisualizationJSON(ctx, email, activity)
	if err := store.SaveReportTranslation(ctx, id, "en", summary); err != nil {
		logger.Warnf("[REPORTS] SaveReportTranslation failed for report %d: %v", id, err)
	}
	if err := store.UpdateReportStatus(ctx, store.ReportStatusCompleted, vizJSON, isTruncated, id, email); err != nil {
		logger.Warnf("[REPORTS] UpdateReportStatus(completed) failed for report %d: %v", id, err)
	}
	if lang != "" && lang != "en" {
		if _, err := s.ProcessOnDemandTranslation(ctx, email, id, lang); err != nil {
			logger.Warnf("[REPORTS] ProcessOnDemandTranslation(%s) failed for report %d: %v", lang, id, err)
		}
	}
}

func (s *ReportsService) markFailed(ctx context.Context, email string, id store.ReportID) {
	if err := store.UpdateReportStatus(ctx, store.ReportStatusFailed, "{}", false, id, email); err != nil {
		logger.Warnf("[REPORTS] UpdateReportStatus(failed) failed for report %d: %v", id, err)
	}
}

// ProcessOnDemandTranslation handles Just-In-Time (JIT) translation for a specific report and language.
// It delegates the heavy lifting to TranslationService while managing report-specific caching.
func (s *ReportsService) ProcessOnDemandTranslation(ctx context.Context, email string, reportID store.ReportID, langCode string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultOnDemandTranslationTimeout)
	defer cancel()

	// 2. Fetch the original report (usually English if it's the fallback)
	report, err := store.GetReportByID(ctx, reportID, email)
	if err != nil {
		return "", fmt.Errorf("failed to fetch original report: %w", err)
	}

	// Double-check the map in the fetched report
	if summary, exists := report.Translations[langCode]; exists {
		return summary, nil
	}

	// 3. Delegate to TranslationService (handles Singleflight internally)
	if s.translationSvc == nil {
		return report.ReportSummary, nil // Return original English as fallback
	}
	key := fmt.Sprintf("report_%d_%s", reportID, langCode)
	translated, err := s.translationSvc.Translate(ctx, email, key, report.ReportSummary, langCode, true, reportID)
	if err != nil {
		return "", fmt.Errorf("AI translation failed: %w", err)
	}

	// 4. Cache in DB
	if err := store.SaveReportTranslation(ctx, reportID, langCode, translated); err != nil {
		logger.Warnf("[REPORTS] cache translation failed: %v", err)
	}

	return translated, nil
}
