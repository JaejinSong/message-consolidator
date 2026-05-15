package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"sort"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

// ReportSummarizer defines the strategy for generating report summaries from logs.
// reportID enables per-report token cost attribution.
type ReportSummarizer interface {
	Generate(ctx context.Context, email, logs string, reportID store.ReportID) (string, error)
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
func (s *FlashSingleSummarizer) Generate(ctx context.Context, email, logs string, reportID store.ReportID) (string, error) {
	return s.gemini.GenerateReportSummary(ctx, email, logs, reportID)
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
	summary, err := s.summarizer.Generate(ctx, email, taskLogs, id)
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

func (s *ReportsService) getVisualizationJSON(ctx context.Context, email string, logs []Log) string {
	// Why: Manual aggregation uses RequesterCanonical as node ID, correctly unifying all aliases resolved by sanitizeMessages.
	// In-process aggregation only — no AI call here. The Gemini-backed GenerateVisualizationData
	// has its own reportID parameter for the day it gets wired into this path.
	vizData := s.generateVisualizationData(ctx, email, logs)
	b, _ := json.Marshal(vizData)
	return string(b)
}

// sanitizeMessages performs batch identity resolution to eliminate N+1 overhead.
func (s *ReportsService) sanitizeMessages(ctx context.Context, email string, msgs []Log) ([]Log, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}

	idsMap := make(map[string]bool)
	for _, m := range msgs {
		idsMap[m.Requester] = true
		idsMap[m.Assignee] = true
	}
	ids := make([]string, 0, len(idsMap))
	for id := range idsMap {
		ids = append(ids, id)
	}

	contacts, ambiguous, err := store.GetContactsByIdentifiers(ctx, email, ids)
	if err != nil {
		return msgs, err
	}

	for i := range msgs {
		m := &msgs[i]
		s.applyResolution(ctx, m, &m.Requester, &m.RequesterCanonical, &m.RequesterDisplayName, &m.RequesterType, contacts, ambiguous)
		s.applyResolution(ctx, m, &m.Assignee, &m.AssigneeCanonical, &m.AssigneeDisplayName, &m.AssigneeType, contacts, ambiguous)
	}
	return msgs, nil
}

func (s *ReportsService) applyResolution(_ context.Context, m *Log, identifierField *string, canonicalField *string, displayNameField *string, typeField *string, contacts map[string]*store.ContactRecord, ambiguous map[string]bool) {
	identifier := *identifierField
	if ambiguous[identifier] {
		*identifierField = identifier + " (Ambiguous)"
		return
	}

	if c, ok := contacts[identifier]; ok {
		*identifierField = c.CanonicalID
		*canonicalField = c.CanonicalID
		*displayNameField = c.DisplayName

		if c.ContactType != "" && c.ContactType != "none" {
			*typeField = c.ContactType
		} else if strings.HasSuffix(strings.ToLower(c.CanonicalID), "@whatap.io") || strings.EqualFold(c.CanonicalID, m.UserEmail) {
			*typeField = store.CategoryInternal
		}
	}
}

// PrepareLogsForAI formats activity and stalled logs into two labelled sections for AI input.
// Activity fills the cutoff budget first; stalled is appended with remaining budget.
func (s *ReportsService) PrepareLogsForAI(email string, activity, stalled []Log) (string, bool) {
	s.sortLogs(activity)
	s.sortLogs(stalled)
	var sb strings.Builder
	curr, truncated := 0, false
	limit := s.config.CutoffSize
	if limit <= 0 {
		limit = DefaultReportCutoffSize
	}

	statsHeader := buildActivityStatsHeader(activity)
	sb.WriteString(statsHeader)
	curr += len(statsHeader)

	activityHeader := "[Activity Tasks]\n"
	sb.WriteString(activityHeader)
	curr += len(activityHeader)

	for _, m := range activity {
		line := s.formatLogLine(email, m)
		if curr+len(line) > limit {
			truncated = true
			break
		}
		sb.WriteString(line)
		curr += len(line)
	}

	stalledHeader := "[Stalled Tasks - active items predating window]\n"
	if !truncated {
		if curr+len(stalledHeader) <= limit {
			sb.WriteString(stalledHeader)
			curr += len(stalledHeader)
			for _, m := range stalled {
				line := s.formatLogLine(email, m)
				if curr+len(line) > limit {
					truncated = true
					break
				}
				sb.WriteString(line)
				curr += len(line)
			}
		} else {
			truncated = true
		}
	}

	return sb.String(), truncated
}

// buildActivityStatsHeader pre-aggregates task counts and top open-task assignees so the model
// can skip that counting work during thinking.
func buildActivityStatsHeader(activity []Log) string {
	done, active := 0, 0
	openCounts := make(map[string]int, len(activity))
	for _, m := range activity {
		if m.Done {
			done++
			continue
		}
		active++
		key := m.AssigneeCanonical
		if key == "" {
			key = m.Assignee
		}
		openCounts[key]++
	}
	type pair struct {
		name string
		n    int
	}
	top := make([]pair, 0, len(openCounts))
	for k, v := range openCounts {
		top = append(top, pair{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	if len(top) > 3 {
		top = top[:3]
	}
	parts := make([]string, len(top))
	for i, p := range top {
		parts[i] = fmt.Sprintf("%s×%d", p.name, p.n)
	}
	owners := strings.Join(parts, ", ")
	if owners == "" {
		owners = "none"
	}
	return fmt.Sprintf("# Stats: %d tasks (%d active, %d done) | Top open assignees: %s\n",
		done+active, active, done, owners)
}

func (s *ReportsService) sortLogs(logs []Log) {
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Done != logs[j].Done {
			return !logs[i].Done
		}
		return logs[i].CreatedAt.After(logs[j].CreatedAt)
	})
}

func (s *ReportsService) formatLogLine(email string, m Log) string {
	status := " "
	if m.Done {
		status = "V"
	}

	reqName := m.RequesterDisplayName
	if reqName == "" {
		reqName = stripParenSuffix(m.Requester)
	}
	reqCat := s.resolveCategory(email, m.RequesterCanonical, m.RequesterType)
	asgName := m.AssigneeDisplayName
	if asgName == "" {
		asgName = stripParenSuffix(m.Assignee)
	}
	asgCat := s.resolveCategory(email, m.AssigneeCanonical, m.AssigneeType)

	cat := m.Category
	if cat == "" {
		cat = "TASK"
	}
	// Why: done tasks are excluded from all evidence-requiring output rules (Type A: active [ ] only;
	// Type B/C: counts and titles; Activity Rule 4: evidence not required for counting).
	// Omitting evidence entirely saves ~73 bytes per done task (~30% of input budget at 6.7x done:active ratio).
	evLen := 0
	if !m.Done {
		evLen = 180
	}
	evidence := ""
	if evLen > 0 {
		evidence = truncateEvidence(m.OriginalText, evLen)
	}

	deadlineStr := ""
	if m.Deadline != "" {
		deadlineStr = ", Due: " + m.Deadline
	}

	// Why: Age is the deterministic signal for the Stalled Tasks rule (working-day cutoff).
	// Done tasks aren't candidates and stays out of the prompt to avoid steering Activity counting.
	ageStr := formatAge(m)

	return fmt.Sprintf("- [%s][%s] %s (Room: %s, From: %s (%s), To: %s (%s)%s%s)%s\n",
		status, cat, m.Task, m.Room, reqName, reqCat, asgName, asgCat, deadlineStr, ageStr, evidence)
}

func formatAge(m Log) string {
	if m.Done {
		return ""
	}
	base := m.CreatedAt
	if !m.AssignedAt.IsZero() && m.AssignedAt.After(base) {
		base = m.AssignedAt
	}
	if base.IsZero() {
		return ""
	}
	days := store.WorkingDaysSince(base, time.Now())
	if days <= 0 {
		return ""
	}
	return fmt.Sprintf(", Age: %dwd", days)
}

// truncateEvidence extracts the newest block from OriginalText (first block post-flip)
// and returns it as a bounded " | Evidence: ..." suffix. Empty if no content.
func truncateEvidence(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "\n\n"); idx != -1 {
		text = text[:idx]
	}
	text = strings.ReplaceAll(text, "\n", " ")
	runes := []rune(text)
	if len(runes) > max {
		runes = runes[:max]
	}
	return " | Evidence: " + string(runes)
}

func (s *ReportsService) resolveCategory(tenantEmail, canonicalID, contactType string) string {
	return store.MapContactType(contactType, strings.ToLower(canonicalID), tenantEmail)
}

type GraphData struct {
	Nodes []Node `json:"nodes"`
	Links []Edge `json:"links"`
}

type Node struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	IsMe     bool    `json:"is_me"`
	Category string  `json:"category"`
}

type Edge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

// generateVisualizationData constructs a weighted network graph from logs.
func (s *ReportsService) generateVisualizationData(ctx context.Context, email string, messages []Log) GraphData {
	counts, pairWeights, meta := s.aggregateRelationsAlt(ctx, email, messages)
	nodes := make([]Node, 0)
	for id, val := range counts {
		nodes = append(nodes, Node{
			ID: id, Name: meta[id].Name,
			Value: val, IsMe: strings.EqualFold(id, email), Category: meta[id].Cat,
		})
	}
	links := make([]Edge, 0)
	for pair, weight := range pairWeights {
		parts := strings.Split(pair, "|")
		links = append(links, Edge{Source: parts[0], Target: parts[1], Weight: weight})
	}
	return GraphData{Nodes: nodes, Links: links}
}

type nodeMeta struct {
	Name string
	Cat  string
}

// stripParenSuffix removes parenthetical content (e.g. "(JJ)", "(Ambiguous)") while preserving original case.
func stripParenSuffix(s string) string {
	if i := strings.Index(s, "("); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func (s *ReportsService) aggregateRelationsAlt(ctx context.Context, email string, messages []Log) (map[string]float64, map[string]float64, map[string]nodeMeta) {
	counts := make(map[string]float64)
	pairWeights := make(map[string]float64)
	meta := make(map[string]nodeMeta)
	for _, m := range messages {
		rID, rName, rCat := s.resolveRelationActor(ctx, email, m.RequesterCanonical, m.RequesterDisplayName, m.RequesterType, m.Requester)
		aID, aName, aCat := s.resolveRelationActor(ctx, email, m.AssigneeCanonical, m.AssigneeDisplayName, m.AssigneeType, m.Assignee)
		if rID == "" || aID == "" || rID == aID {
			continue
		}
		counts[rID]++
		counts[aID]++
		pairWeights[rID+"|"+aID]++
		meta[rID] = nodeMeta{rName, rCat}
		meta[aID] = nodeMeta{aName, aCat}
	}
	return counts, pairWeights, meta
}

// Why: Prefer the persisted canonical/display/category triple, but fall back to NormalizeWithCategory when the canonical ID is missing
// or when the persisted category is "External" with no explicit type — that fallback is the only path that re-classifies a contact.
func (s *ReportsService) resolveRelationActor(ctx context.Context, email, canonicalID, displayName, contactType, raw string) (string, string, string) {
	id := canonicalID
	name := displayName
	cat := s.resolveCategory(email, id, contactType)
	switch {
	case id == "":
		id, name, cat = store.NormalizeWithCategory(ctx, email, raw)
	case cat == "External" && contactType == "":
		fallback := displayName
		if fallback == "" {
			fallback = raw
		}
		if _, _, c := store.NormalizeWithCategory(ctx, email, fallback); c != "External" {
			cat = c
		}
	}
	if name == "" {
		name = raw
	}
	return id, stripParenSuffix(name), cat
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
