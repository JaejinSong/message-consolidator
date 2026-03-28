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

type ReportsService struct {
	gemini *ai.GeminiClient
}

func NewReportsService(gemini *ai.GeminiClient) *ReportsService {
	return &ReportsService{gemini: gemini}
}

// Why: Orchestrates the generation of an AI-powered work report by checking the database cache first, and then coordinating between the Go-based visualization engine and the Gemini summary API.
func (s *ReportsService) GetWeeklyReport(ctx context.Context, email string) (*store.Report, error) {
	now := time.Now()
	startDate := now.AddDate(0, 0, -7).Format("2006-01-02")
	endDate := now.Format("2006-01-02")

	// 1. Check Cache
	cached, err := store.GetReport(ctx, email, startDate, endDate)
	if err == nil && cached != nil {
		// Why: Validates the cached report based on a 6-hour TTL to ensure freshness while significantly reducing API costs for frequently accessed insights.
		if time.Since(cached.CreatedAt) < 6*time.Hour {
			logger.Infof("[REPORTS] Cache hit for %s (%s ~ %s)", email, startDate, endDate)
			return cached, nil
		}
	}

	// 2. Fetch Data
	sinceDate := now.AddDate(0, 0, -7)
	messages, err := store.GetMessagesForReport(ctx, email, sinceDate)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages found for the report period (last 7 days)")
	}

	// 3. Generate Visualization (Go Backend)
	vizData := s.generateVisualizationData(messages)
	vizJSON, _ := json.Marshal(vizData)

	// 4. Summarize for AI (Byte-based Truncation)
	// Why: [Safe Margin] Uses 15KB as a safe limit for the task summary payload to ensure AI prompt fits comfortably within the 4096 output token constraint's context.
	taskSummary := s.prepareTaskSummaryForAI(messages, 15000)

	// 5. Call Gemini
	summary, err := s.gemini.GenerateReportSummary(ctx, email, taskSummary)
	if err != nil {
		return nil, err
	}

	// 6. Save and Return
	report := &store.Report{
		UserEmail:     email,
		StartDate:     startDate,
		EndDate:       endDate,
		Summary:       summary,
		Visualization: string(vizJSON),
	}

	err = store.SaveReport(ctx, report)
	if err != nil {
		logger.Warnf("[REPORTS] Failed to save report to cache: %v", err)
	}

	return report, nil
}

// Why: Implements a strict byte-length-based truncator that slices the task list until it fits within a safe limit for the AI API, preventing 400 Bad Request errors.
func (s *ReportsService) prepareTaskSummaryForAI(messages []store.ConsolidatedMessage, maxBytes int) string {
	var sb strings.Builder
	currentBytes := 0

	// Sort messages: Prioritize Incomplete (Done == false), then newest first (CreatedAt DESC)
	// Why: Ensures critical pending items are always included in the summary even if the context window limit (15KB) is reached.
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].Done != messages[j].Done {
			// Done == false (0) comes before Done == true (1)
			return !messages[i].Done
		}
		return messages[i].CreatedAt.After(messages[j].CreatedAt)
	})

	for _, m := range messages {
		status := " "
		if m.Done {
			status = "V"
		}
		// Why: Only includes critical fields (status, task, requester, assignee) to maximize information density within the byte limit.
		line := fmt.Sprintf("- [%s] %s (From: %s, To: %s, Date: %s)\n",
			status, m.Task, m.Requester, m.Assignee, m.CreatedAt.Format("01-02"))

		lineBytes := len([]byte(line))
		if currentBytes+lineBytes > maxBytes {
			break
		}
		sb.WriteString(line)
		currentBytes += lineBytes
	}

	return sb.String()
}

type GraphData struct {
	Nodes []Node `json:"nodes"`
	Links []Edge `json:"links"` // ECharts 표준 규격인 links로 변경
}

type Node struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	IsMe  bool    `json:"is_me"` // 프론트엔드 하이라이팅용
}

type Edge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

// Why: Aggregates requester-assignee relationships from the message logs to construct a weighted network graph, reducing LLM output overhead and ensuring data consistency.
func (s *ReportsService) generateVisualizationData(messages []store.ConsolidatedMessage) GraphData {
	counts := make(map[string]float64)
	pairWeights := make(map[string]float64) // "Source|Target" -> weight

	for _, m := range messages {
		req := strings.TrimSpace(m.Requester)
		asg := strings.TrimSpace(m.Assignee)
		// Why: Skips empty or self-referenced relationships to ensure the network graph focuses on meaningful collaborative communications.
		if req == "" || asg == "" || req == asg {
			continue
		}

		counts[req]++
		counts[asg]++

		pair := req + "|" + asg
		pairWeights[pair]++
	}

	nodes := make([]Node, 0) // 빈 배열일 때 null이 되지 않도록 초기화
	for id, val := range counts {
		nodes = append(nodes, Node{
			ID:    id,
			Name:  id,
			Value: val,
			IsMe:  strings.ToLower(id) == "me",
		})
	}

	links := make([]Edge, 0) // 빈 배열일 때 null이 되지 않도록 초기화
	for pair, weight := range pairWeights {
		parts := strings.Split(pair, "|")
		links = append(links, Edge{Source: parts[0], Target: parts[1], Weight: weight})
	}

	return GraphData{Nodes: nodes, Links: links}
}
