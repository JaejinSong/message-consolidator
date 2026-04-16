package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"sort"
	"strings"
	"time"
)

// ReportSummarizer defines the strategy for generating report summaries from logs.
type ReportSummarizer interface {
	Generate(ctx context.Context, logs string) (string, error)
}

// ReportConfig encapsulates configuration parameters for the report service.
type ReportConfig struct {
	CutoffSize int
}

const DefaultReportCutoffSize = 16000

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
func (s *FlashSingleSummarizer) Generate(ctx context.Context, logs string) (string, error) {
	// Note: email is not passed here as per the requested interface signature.
	// GeminiClient's GenerateReportSummary currently uses email for token usage logging.
	// We'll pass an empty string or consider refactoring GeminiClient if persistent user-tracking is needed here.
	return s.gemini.GenerateReportSummary(ctx, "", logs)
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

// Why: Orchestrates the generation of an AI-powered work report.
func (s *ReportsService) GenerateReport(ctx context.Context, email, start, end, lang string, source *string, done *bool) (*store.Report, error) {
	// 1. Check for processing or existing
	// Note: We ignore cache for filtered reports as the date-based cache in GetReportByDate
	// currently doesn't account for source/status filters.
	if source == nil && done == nil {
		if existing, _ := store.GetReportByDate(ctx, email, start); existing != nil {
			if existing.Status == "processing" || existing.Status == "completed" {
				if existing.Status == "completed" {
					existing.Translations, _ = store.GetReportTranslations(ctx, existing.ID)
				}
				return existing, nil
			}
		}
	}

	// 2. Fetch and sanitize
	filtered, err := s.fetchAndFilterMessages(ctx, email, start, end, source, done)
	if err != nil {
		return nil, err
	}
	s.sanitizeMessages(ctx, email, filtered) // Ignore error, self-healing

	// 3. Create Placeholder
	report := &store.Report{
		UserEmail: email, StartDate: start, EndDate: end,
		Status: "processing", Visualization: "{}", Translations: make(map[string]string),
	}
	id, err := store.SaveReport(ctx, report)
	if err != nil {
		return nil, err
	}
	report.ID = int(id)

	// 4. Background Job
	if s.isTest {
		s.processAsyncReport(email, start, end, lang, report.ID, filtered)
		// 💡 Sync update for test: Re-fetch report to ensure all fields (Status, Summary, Translations) are refreshed
		refreshed, err := store.GetReportByID(ctx, report.ID, email)
		if err == nil {
			*report = *refreshed
		}
	} else {
		go s.processAsyncReport(email, start, end, lang, report.ID, filtered)
	}

	return report, nil
}

func (s *ReportsService) fetchAndFilterMessages(ctx context.Context, email, startDate, endDate string, source *string, done *bool) ([]Log, error) {
	start, _ := time.Parse("2006-01-02", startDate)
	messages, err := store.GetMessagesForReport(ctx, email, start, source, done)
	if err != nil {
		return nil, err
	}
	var filtered []Log
	for _, m := range messages {
		ds := m.CreatedAt.Format("2006-01-02")
		if ds >= startDate && ds <= endDate {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no messages found for %s ~ %s (source: %v, done: %v)", startDate, endDate, source, done)
	}
	return filtered, nil
}

func (s *ReportsService) processAsyncReport(email, start, end, lang string, id int, logs []Log) {
	ctx := context.Background()
	taskLogs, isTruncated := s.PrepareLogsForAI(email, logs)
	summary, err := s.summarizer.Generate(ctx, taskLogs)
	if err != nil {
		s.markFailed(ctx, email, id)
		return
	}
	// Extract content and visualization JSON
	vizJSON, text, _ := ExtractJSONBlock(summary)
	if text == "" {
		text = summary
	}
	if vizJSON == "" {
		vData := s.generateVisualizationData(email, logs)
		b, _ := json.Marshal(vData)
		vizJSON = string(b)
	}
	// Save results and handle translations
	store.SaveReportTranslation(ctx, int64(id), "en", text)
	store.UpdateReportStatus(ctx, "completed", vizJSON, isTruncated, id, email)
	if lang != "" && lang != "en" {
		s.ProcessOnDemandTranslation(ctx, email, id, lang)
	}
}

func (s *ReportsService) markFailed(ctx context.Context, email string, id int) {
	store.UpdateReportStatus(ctx, "failed", "{}", false, id, email)
}

func (s *ReportsService) getVisualizationJSON(email string, logs []Log, aiJSON string) string {
	if aiJSON != "" {
		var gData GraphData
		if err := json.Unmarshal([]byte(aiJSON), &gData); err == nil && len(gData.Nodes) > 0 {
			b, _ := json.Marshal(gData)
			return string(b)
		}
	}
	// Fallback to manual aggregation
	vizData := s.generateVisualizationData(email, logs)
	b, _ := json.Marshal(vizData)
	return string(b)
}

func (s *ReportsService) getGeminiClient() *ai.GeminiClient {
	if fs, ok := s.summarizer.(*FlashSingleSummarizer); ok {
		return fs.gemini
	}
	return nil
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

	contacts, ambiguous, _ := store.GetContactsByIdentifiers(ctx, email, ids)

	for i := range msgs {
		m := &msgs[i]
		s.applyResolution(ctx, m, &m.Requester, &m.RequesterCanonical, &m.RequesterDisplayName, &m.RequesterType, contacts, ambiguous, true)
		s.applyResolution(ctx, m, &m.Assignee, &m.AssigneeCanonical, &m.AssigneeDisplayName, &m.AssigneeType, contacts, ambiguous, false)
	}
	return msgs, nil
}

func (s *ReportsService) applyResolution(ctx context.Context, m *Log, identifierField *string, canonicalField *string, displayNameField *string, typeField *string, contacts map[string]*store.ContactRecord, ambiguous map[string]bool, isRequester bool) {
	identifier := *identifierField
	if ambiguous[identifier] {
		*identifierField = identifier + " (Ambiguous)"
		return
	}

	if c, ok := contacts[identifier]; ok {
		// 💡 Self-Healing: Update DB if non-canonical ID was used.
		if identifier != c.CanonicalID && identifier != c.DisplayName {
			go func() {
				req, asg := "", ""
				if isRequester {
					req = c.CanonicalID
				} else {
					asg = c.CanonicalID
				}
				if err := store.UpdateMessageIdentity(context.Background(), store.GetDB(), m.UserEmail, m.Room, m.ID, req, asg); err != nil {
					logger.Errorf("[REPORTS] Identity self-healing failed for task %d: %v", m.ID, err)
				}
			}()
		}
		*identifierField = c.CanonicalID // Normalized to Email for DB consistency and tests
		*canonicalField = c.CanonicalID
		*displayNameField = c.DisplayName // Preserved for UI/Visualization

		// 💡 Promotion: Use contact_type from mapping if present
		if c.ContactType != "" && c.ContactType != "none" {
			*typeField = c.ContactType
		}
	}
}

// PrepareLogsForAI formats logs for AI input, respecting the 8,000 character cutoff.
func (s *ReportsService) PrepareLogsForAI(email string, rawLogs []Log) (string, bool) {
	s.sortLogs(rawLogs)
	var sb strings.Builder
	curr, truncated := 0, false
	limit := s.config.CutoffSize
	if limit <= 0 {
		limit = DefaultReportCutoffSize
	}

	for _, m := range rawLogs {
		line := s.formatLogLine(email, m)
		if curr+len(line) > limit {
			truncated = true
			break
		}
		sb.WriteString(line)
		curr += len(line)
	}
	return sb.String(), truncated
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

	reqName := m.Requester
	reqCat := s.resolveCategory(email, m.RequesterCanonical, m.RequesterType)
	asgName := m.Assignee
	asgCat := s.resolveCategory(email, m.AssigneeCanonical, m.AssigneeType)

	return fmt.Sprintf("- [%s] %s (From: %s (%s), To: %s (%s))\n",
		status, m.Task, reqName, reqCat, asgName, asgCat)
}

func (s *ReportsService) resolveCategory(tenantEmail, canonicalID, contactType string) string {
	if contactType == "internal" || strings.EqualFold(canonicalID, tenantEmail) {
		return "Internal"
	}
	return "External"
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
func (s *ReportsService) generateVisualizationData(email string, messages []Log) GraphData {
	counts, pairWeights, meta := s.aggregateRelationsAlt(email, messages)
	nodes := make([]Node, 0)
	for id, val := range counts {
		nodes = append(nodes, Node{
			ID: id, Name: fmt.Sprintf("%s (%s)", meta[id].Name, meta[id].Cat),
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

func (s *ReportsService) aggregateRelationsAlt(email string, messages []Log) (map[string]float64, map[string]float64, map[string]nodeMeta) {
	counts := make(map[string]float64)
	pairWeights := make(map[string]float64)
	meta := make(map[string]nodeMeta)
	for _, m := range messages {
		// Why: Prioritize canonical IDs for node unification; fallback to raw requester/assignee strings if not resolved.
		rID := m.RequesterCanonical
		if rID == "" {
			rID = strings.ToLower(m.Requester)
		}
		rCat := s.resolveCategory(email, rID, m.RequesterType)
		rName := m.RequesterDisplayName
		if rName == "" {
			rName = m.Requester
		}

		aID := m.AssigneeCanonical
		if aID == "" {
			aID = strings.ToLower(m.Assignee)
		}
		aCat := s.resolveCategory(email, aID, m.AssigneeType)
		aName := m.AssigneeDisplayName
		if aName == "" {
			aName = m.Assignee
		}

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

// ProcessOnDemandTranslation handles Just-In-Time (JIT) translation for a specific report and language.
// It delegates the heavy lifting to TranslationService while managing report-specific caching.
func (s *ReportsService) ProcessOnDemandTranslation(ctx context.Context, email string, reportID int, langCode string) (string, error) {
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
	translated, err := s.translationSvc.Translate(ctx, email, key, report.ReportSummary, langCode, true)
	if err != nil {
		return "", fmt.Errorf("AI translation failed: %w", err)
	}

	// 4. Cache in DB
	if err := store.SaveReportTranslation(ctx, int64(reportID), langCode, translated); err != nil {
		logger.Errorf("[REPORTS] Failed to cache translation: %v", err)
	}

	return translated, nil
}
