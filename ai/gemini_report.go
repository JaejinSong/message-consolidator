package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
	"google.golang.org/genai"
)

// Why: Summarizes a list of tasks into a structured Markdown business report.
func (g *GeminiClient) GenerateReportSummary(ctx context.Context, email string, tasks string, reportID store.ReportID) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptReportSummary)
	data := core.ExtractionContext{
		MessagePayload:   tasks,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		StaleThreshold:   store.GetStaleThresholdWorkingDays(),
		CurrentUserEmail: email,
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		return "", fmt.Errorf("failed to render report summary prompt: %w", err)
	}

	modelName := g.getEffectiveModel(parsed, g.analysisModel)
	cfg := g.buildConfig(0.1, ReportMaxTokens, "", rendered)
	// Why: ThinkingBudget does not cap thinking in practice — gemini-3-flash-preview used
	// 39,317 tokens thinking even with budget=16384 set (report_id=82). The real guard is the
	// enlarged MaxOutputTokens (65536) which absorbs ~39K thinking and leaves ~26K for completion.
	cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(int32(0))}

	start := time.Now()
	// Why: empty string Part is rejected by the API as an uninitialized oneof field (INVALID_ARGUMENT).
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text("."), cfg, 180*time.Second, 2)
	if err != nil {
		// P1: Surface burned-but-unattributed retry-exhausted calls so the cost dashboard
		// can flag invisible spend. Gemini does not return UsageMetadata on timeout/cancel.
		if uErr := store.AddTokenUsage(email, "ReportSummary", modelName, "failed", reportID, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] ReportSummary failure attribution: %v", uErr)
		}
		return "", err
	}

	logTokenUsage(ctx, email, "ReportSummary", modelName, "", reportID, resp)
	if len(resp.Candidates) > 0 {
		if fr := resp.Candidates[0].FinishReason; fr == genai.FinishReasonMaxTokens {
			logger.Warnf("[GEMINI] ReportSummary hit MAX_TOKENS: thinking=%d completion=%d prompt=%d budget=%d email=%s",
				resp.UsageMetadata.ThoughtsTokenCount, resp.UsageMetadata.CandidatesTokenCount,
				resp.UsageMetadata.PromptTokenCount, ReportMaxTokens, email)
		}
	}
	text, err := extractResponseText(resp)
	if err != nil {
		return "", err
	}
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason == genai.FinishReasonMaxTokens {
		text = repairTruncatedOutput(text)
	}

	_ = trace.Step(ctx, "Gemini-ReportSummary", "", int(time.Since(start).Milliseconds()), 0)
	return text, nil
}

// repairTruncatedOutput closes any open JSON code fence and strips the last incomplete
// JSON object so the partial list can still render as a Slack table block.
func repairTruncatedOutput(text string) string {
	const notice = "\n\n> ⚠️ 보고서가 출력 토큰 한도에 도달해 일부 내용이 잘렸습니다."
	if strings.Count(text, "```")%2 == 0 {
		return text + notice
	}
	lastFence := strings.LastIndex(text, "```")
	beforeFence := text[:lastFence]
	fenceContent := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[lastFence+3:]), "json"))
	if strings.HasPrefix(fenceContent, "[") {
		// Strip last incomplete object: find the rightmost complete item boundary.
		idx := strings.LastIndex(fenceContent, "},")
		if idx < 0 {
			idx = strings.LastIndex(fenceContent, "}")
		}
		if idx >= 0 {
			fenceContent = fenceContent[:idx+1] + "\n]"
		} else {
			fenceContent = "[]"
		}
		text = beforeFence + "```json\n" + fenceContent + "\n```"
	} else {
		text = text + "\n```"
	}
	return text + notice
}

// EvaluateTaskTransition determines if a reply completes or updates a specific parent task.
// Why: [Thread-Aware Intelligence] Uses a specialized prompt to analyze the conversational relationship
// between a parent message and its reply, enabling deterministic state transitions (RESOLVE/UPDATE).
func (g *GeminiClient) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (TaskTransition, error) {
	if g == nil || g.client == nil {
		return TaskTransition{}, fmt.Errorf("Gemini client not initialized")
	}

	parsed := core.LoadPrompt(core.PromptCompletionCheck)
	var subtasksCtx string
	if len(subtasks) > 0 && len(subtasks) <= 5 {
		for i, s := range subtasks {
			mark := "[ ]"
			if s.Done {
				mark = "[x]"
			}
			subtasksCtx += fmt.Sprintf("%d. %s %s\n", i, mark, s.Task)
		}
	}
	data := core.ExtractionContext{
		ParentTask:      parentTask,
		MessagePayload:  replyText,
		SubtasksContext: subtasksCtx,
		CurrentTime:     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:          "Korean",
	}

	rendered, err := parsed.Render(data)
	if err != nil {
		return TaskTransition{}, fmt.Errorf("failed to render completion prompt: %w", err)
	}

	modelName := g.getEffectiveModel(parsed, g.analysisModel)
	cfg := g.buildConfig(0.1, 1024, "application/json", rendered)
	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text("."), cfg, 30*time.Second, 2)
	if err != nil {
		return TaskTransition{}, err
	}

	_ = trace.Step(ctx, "Gemini-EvaluateTransition", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "EvaluateTransition", modelName, "", 0, resp)
	raw, err := extractResponseText(resp)
	if err != nil {
		return TaskTransition{}, err
	}

	var result TaskTransition
	if err := json.Unmarshal([]byte(core.SanitizeJSON(raw)), &result); err != nil {
		return TaskTransition{}, fmt.Errorf("failed to parse AI transition response: %w (raw: %s)", err, raw)
	}

	return result, nil
}

// GenerateVisualizationData extracts graph structural data using strict ResponseSchema enforcement.
// Why: [Hallucination Defense] Eliminates invalid JSON by forcing the model to adhere to a predefined schema.
func (g *GeminiClient) GenerateVisualizationData(ctx context.Context, email string, tasks string, reportID store.ReportID) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client not initialized")
	}

	schema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"nodes": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id":       {Type: genai.TypeString},
						"name":     {Type: genai.TypeString},
						"value":    {Type: genai.TypeNumber},
						"category": {Type: genai.TypeString},
					},
					Required: []string{"id", "name", "value", "category"},
				},
			},
			"links": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"source": {Type: genai.TypeString},
						"target": {Type: genai.TypeString},
						"weight": {Type: genai.TypeNumber},
					},
					Required: []string{"source", "target", "weight"},
				},
			},
		},
		Required: []string{"nodes", "links"},
	}

	cfg := g.buildConfig(0.0, 4096, "application/json", "Generate a JSON graph of task relations.")
	cfg.ResponseSchema = schema

	prompt := fmt.Sprintf("Extract task network graph (Nodes=People, Links=Handover/Mention) from these logs:\n%s", tasks)
	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, g.analysisModel, genai.Text(prompt), cfg, 60*time.Second, 2)
	if err != nil {
		return "", err
	}

	_ = trace.Step(ctx, "Gemini-Visualization", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "ReportVizData", g.analysisModel, "", reportID, resp)
	return extractResponseText(resp)
}

// GenerateMergedTaskTitle summarizes multiple task titles and messages into a single English title.
// Why: [Unified Consistency] Strictly enforces 30-character English limit via AI for unified task presentation.
func (g *GeminiClient) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client not initialized")
	}

	parsed := core.LoadPrompt(core.PromptTaskMergeSummary)
	data := core.ExtractionContext{
		MessagePayload: tasksJSON,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "English",
	}

	rendered, err := parsed.Render(data)
	if err != nil {
		return "", fmt.Errorf("failed to render merge summary prompt: %w", err)
	}

	// Why: [Performance] gemini-3-flash-preview is used for high-speed, high-quality short-form summary.
	modelName := g.getEffectiveModel(parsed, "gemini-3-flash-preview")
	cfg := g.buildConfig(0.1, 100, "", rendered)

	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text("."), cfg, 10*time.Second, 1)
	if err != nil {
		return "", err
	}

	logTokenUsage(ctx, email, "MergeSummary", modelName, "", 0, resp)
	text, err := extractResponseText(resp)
	if err != nil {
		return "", err
	}

	_ = trace.Step(ctx, "Gemini-MergeSummary", "", int(time.Since(start).Milliseconds()), 0)
	return core.CleanMarkdownText(text), nil
}
