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
}

func NewReportsService(summarizer ReportSummarizer, geminiClient *ai.GeminiClient, trans *TranslationService, config ReportConfig) *ReportsService {
	return &ReportsService{
		summarizer:     summarizer,
		geminiClient:   geminiClient,
		translationSvc: trans,
		config:         config,
	}
}

// Why: Orchestrates the generation of an AI-powered work report by coordinating between the Go-based visualization engine and the injected summarizer strategy.
// Now supports a 1:N multi-language pipeline (EN, KR, ID, TH).
func (s *ReportsService) GenerateReport(ctx context.Context, email, startDate, endDate string) (*store.Report, error) {
	// 1. Parse dates
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start date: %w", err)
	}

	// 2. Fetch Data
	messages, err := store.GetMessagesForReport(ctx, email, start)
	if err != nil {
		return nil, err
	}

	var filtered []Log
	for _, m := range messages {
		dateStr := m.CreatedAt.Format("2006-01-02")
		if dateStr >= startDate && dateStr <= endDate {
			filtered = append(filtered, m)
		}
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("no messages found for the period %s ~ %s", startDate, endDate)
	}

	// [Self-Healing] 파편화된 식별자 정규화
	s.sanitizeMessages(ctx, email, filtered)

	// 3. Generate Visualization (Go Backend)
	vizData := s.generateVisualizationData(email, filtered)
	vizJSON, _ := json.Marshal(vizData)

	// 4. Transform Data for AI
	taskSummary, isTruncated := s.PrepareLogsForAI(email, filtered)

	// 5. Generate Base Summary (English)
	summaryEN, err := s.summarizer.Generate(ctx, taskSummary)
	if err != nil {
		return nil, err
	}

	// 6. Create & Save Report Metadata
	report := &store.Report{
		UserEmail:     email,
		StartDate:     startDate,
		EndDate:       endDate,
		Summary:       summaryEN,
		Visualization: string(vizJSON),
		IsTruncated:   isTruncated,
	}

	// [Step 1] Save Metadata & Base Translation
	reportID, err := store.SaveReport(ctx, report)
	if err != nil {
		return nil, fmt.Errorf("failed to save report metadata: %w", err)
	}
	report.ID = int(reportID)

	err = store.SaveReportTranslation(ctx, reportID, "en", summaryEN)
	if err != nil {
		logger.Warnf("[REPORTS] Failed to save English translation: %v", err)
	}
	report.Translations = make(map[string]string)
	report.Translations["en"] = summaryEN

	// [Step 2] Sequentially Translate & Save for Target Languages
	// Why: Defines standardized ISO 639-1 language codes for multi-language report generation.
	targetLanguages := map[string]string{
		"ko": "Korean",
		"id": "Indonesian",
		"th": "Thai",
	}
	
	// Access gemini client directly from summarizer if possible
	var gemini *ai.GeminiClient
	if fs, ok := s.summarizer.(*FlashSingleSummarizer); ok {
		gemini = fs.gemini
	}

	if gemini != nil {
		for code, langName := range targetLanguages {
			translated, err := gemini.TranslateReport(ctx, email, summaryEN, langName)
			if err != nil {
				logger.Errorf("[REPORTS] Translation to %s failed for report %d: %v", langName, reportID, err)
				continue
			}
			
			if err := store.SaveReportTranslation(ctx, reportID, code, translated); err != nil {
				logger.Errorf("[REPORTS] Saving translation (%s) failed for report %d: %v", code, reportID, err)
			} else {
				report.Translations[code] = translated
			}
		}
	}

	return report, nil
}

// sanitizeMessages performs real-time identity normalization on message logs and triggers asynchronous "Self-Healing" DB updates.
func (s *ReportsService) sanitizeMessages(ctx context.Context, tenantEmail string, messages []Log) {
	for i := range messages {
		msg := &messages[i]
		
		// Why: Encapsulates resolution logic to handle both successful healing and ambiguity safeguards.
		resolve := func(identifier string, field *string) bool {
			if identifier == "" || strings.Contains(identifier, "@") {
				return false
			}
			c, err := store.GetContactByIdentifier(tenantEmail, identifier)
			if err != nil {
				// 💡 Ambiguity Safeguard: Handle duplicate name/alias hits.
				if ambigErr, ok := err.(*store.AmbiguousIdentityError); ok {
					logger.Warnf("[AMBIGUOUS_CONFLICT] Name: %s, Emails: %v", identifier, ambigErr.Emails)
					*field = identifier + " (Ambiguous)"
					return false // Do NOT attempt DB update.
				}
				return false
			}
			if c != nil {
				logger.Infof("[Self-Healed] ID:%d, %s -> %s", msg.ID, identifier, c.CanonicalID)
				*field = c.CanonicalID
				return true
			}
			return false
		}

		// Initial states to preserve original values for resolution check
		origReq := msg.Requester
		origAsg := msg.Assignee

		reqHealed := resolve(origReq, &msg.Requester)
		asgHealed := resolve(origAsg, &msg.Assignee)

		// 3. Asynchronously heal the original data in the database (Only for healed records)
		if reqHealed || asgHealed {
			go func(id int, req, asg string) {
				db := store.GetDB()
				_, err := db.ExecContext(context.Background(), store.SQL.UpdateMessageIdentity, req, asg, id)
				if err != nil {
					logger.Errorf("[Self-Healing-Error] Failed to update record ID:%d: %v", id, err)
				}
			}(msg.ID, msg.Requester, msg.Assignee)
		}
	}
}

// PrepareLogsForAI implements the "Transform" stage by normalizing raw logs and formatting them for the AI summarizer.
// It respects the 8,000 character cutoff defined for AI input stability.
func (s *ReportsService) PrepareLogsForAI(email string, rawLogs []Log) (string, bool) {
	var sb strings.Builder
	currentBytes := 0
	isTruncated := false
	const AISizeCutoff = 8000

	// Sort logs: Prioritize Incomplete (Done == false), then newest first (CreatedAt DESC)
	sort.Slice(rawLogs, func(i, j int) bool {
		if rawLogs[i].Done != rawLogs[j].Done {
			return !rawLogs[i].Done
		}
		return rawLogs[i].CreatedAt.After(rawLogs[j].CreatedAt)
	})

	for _, m := range rawLogs {
		status := " "
		if m.Done {
			status = "V"
		}
		// Why: Re-evaluate identity with SSOT (contacts cache) to handle potentially contaminated legacy database entries.
		_, reqName, reqCat := store.NormalizeWithCategory(email, m.Requester)
		_, asgName, asgCat := store.NormalizeWithCategory(email, m.Assignee)

		// Why: Force "Name (Category)" format for both AI logs and visualization to ensure context clarity.
		line := fmt.Sprintf("- [%s] %s (From: %s (%s), To: %s (%s))\n",
			status, m.Task, reqName, reqCat, asgName, asgCat)

		lineBytes := len([]byte(line))
		if currentBytes+lineBytes > AISizeCutoff {
			isTruncated = true
			break
		}
		sb.WriteString(line)
		currentBytes += lineBytes
	}

	return sb.String(), isTruncated
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

// Why: Aggregates requester-assignee relationships from the message logs to construct a weighted network graph.
func (s *ReportsService) generateVisualizationData(email string, messages []Log) GraphData {
	counts := make(map[string]float64)
	pairWeights := make(map[string]float64)
	idToName := make(map[string]string)
	idToCategory := make(map[string]string)

	for _, m := range messages {
		// [중요] 집계 시작 직전에 두 사람 모두 정규화 수행
		reqID, reqName, reqCat := store.NormalizeWithCategory(email, m.Requester)
		asgID, asgName, asgCat := store.NormalizeWithCategory(email, m.Assignee)

		// Why: Eliminate self-loops and empty IDs to prevent circular dependencies and invalid data.
		if reqID == "" || asgID == "" || reqID == asgID {
			continue
		}

		// 모든 키는 소문자 이메일(ID)로 통일하여 집계
		counts[reqID]++
		counts[asgID]++

		pair := reqID + "|" + asgID
		pairWeights[pair]++

		// Why: Cache the resolved name and category to avoid re-calculating them when creating nodes.
		idToName[reqID] = reqName
		idToCategory[reqID] = reqCat
		idToName[asgID] = asgName
		idToCategory[asgID] = asgCat
	}

	nodes := make([]Node, 0)
	for id, val := range counts {
		name := idToName[id]
		category := idToCategory[id]
		
		nodes = append(nodes, Node{
			ID:    id,
			Name:  fmt.Sprintf("%s (%s)", name, category), // [CRITICAL] 이름 옆에 카테고리 병기 강제
			Value: val,
			IsMe:  strings.EqualFold(id, email),
			Category: category,
		})
	}

	links := make([]Edge, 0)
	for pair, weight := range pairWeights {
		parts := strings.Split(pair, "|")
		// Why: Contract enforcement ensures links.source/target match nodes.id exactly.
		links = append(links, Edge{
			Source: parts[0],
			Target: parts[1],
			Weight: weight,
		})
	}

	return GraphData{Nodes: nodes, Links: links}
}

// ProcessOnDemandTranslation handles Just-In-Time (JIT) translation for a specific report and language.
// It delegates the heavy lifting to TranslationService while managing report-specific caching.
func (s *ReportsService) ProcessOnDemandTranslation(ctx context.Context, email string, reportID int, langCode string) (string, error) {
	// 1. Check DB cache first
	translations, err := store.GetReportTranslations(ctx, reportID)
	if err == nil {
		if summary, exists := translations[langCode]; exists {
			return summary, nil
		}
	}

	// 2. Fetch the original report (usually English if it's the fallback)
	report, err := store.GetReportByID(ctx, reportID, email)
	if err != nil {
		return "", fmt.Errorf("failed to fetch original report: %w", err)
	}

	// 3. Delegate to TranslationService (handles Singleflight internally)
	key := fmt.Sprintf("report_%d_%s", reportID, langCode)
	translated, err := s.translationSvc.Translate(ctx, email, key, report.Summary, langCode, true)
	if err != nil {
		return "", fmt.Errorf("AI translation failed: %w", err)
	}

	// 4. Cache in DB
	if err := store.SaveReportTranslation(ctx, int64(reportID), langCode, translated); err != nil {
		logger.Errorf("[REPORTS] Failed to cache translation: %v", err)
	}

	return translated, nil
}

func getLanguageName(code string) string {
	switch strings.ToLower(code) {
	case "ko":
		return "Korean"
	case "en":
		return "English"
	case "id":
		return "Indonesian"
	case "th":
		return "Thai"
	default:
		return code
	}
}
