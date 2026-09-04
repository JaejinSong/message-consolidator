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
)

// Why: Summarizes a list of tasks into a structured Markdown business report.
func (g *AIClient) GenerateReportSummary(ctx context.Context, email string, tasks string, window string, reportID store.ReportID) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptReportSummary)
	data := core.ExtractionContext{
		MessagePayload:   tasks,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		StaleThreshold:   store.GetStaleThresholdWorkingDays(),
		CurrentUserEmail: email,
		ReportWindow:     window,
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		return "", fmt.Errorf("failed to render report summary prompt: %w", err)
	}

	modelName := g.resolveModel(parsed, g.report)
	req := LLMRequest{
		Model:       modelName,
		System:      rendered,
		Temperature: 0.1,
		MaxTokens:   ReportMaxTokens,
		Thinking:    g.resolveThinking(parsed, g.report),
	}

	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, reportGenerateTimeout, 2)
	if err != nil {
		// P1: Surface burned-but-unattributed retry-exhausted calls so the cost dashboard
		// can flag invisible spend. Providers do not return usage on timeout/cancel.
		if uErr := store.AddTokenUsage(email, "ReportSummary", modelName, "failed", reportID, 0, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] ReportSummary failure attribution: %v", uErr)
		}
		return "", err
	}

	logTokenUsage(ctx, email, "ReportSummary", modelName, "", reportID, resp.Usage)
	if resp.FinishReason == "length" {
		logger.Warnf("[AI] ReportSummary hit output limit: think=%d completion=%d prompt=%d budget=%d email=%s",
			resp.Usage.ReasoningTokens, resp.Usage.CompletionTokens, resp.Usage.PromptTokens, ReportMaxTokens, email)
	}
	text := resp.Text
	if resp.FinishReason == "length" {
		text = repairTruncatedOutput(text)
	}

	_ = trace.Step(ctx, g.tracePrefix+"-ReportSummary", "", int(time.Since(start).Milliseconds()), 0)
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
	if !strings.HasPrefix(fenceContent, "[") {
		return text + "\n```" + notice
	}
	return beforeFence + "```json\n" + closeJSONArray(fenceContent) + "\n```" + notice
}

// closeJSONArray trims a partially written JSON array back to its last complete item and
// closes it, yielding "[]" when not even one item finished.
func closeJSONArray(fenceContent string) string {
	idx := strings.LastIndex(fenceContent, "},")
	if idx < 0 {
		idx = strings.LastIndex(fenceContent, "}")
	}
	if idx < 0 {
		return "[]"
	}
	return fenceContent[:idx+1] + "\n]"
}

// EvaluateTaskTransition determines if a reply completes or updates a specific parent task.
// Why: [Thread-Aware Intelligence] Uses a specialized prompt to analyze the conversational relationship
// between a parent message and its reply, enabling deterministic state transitions (RESOLVE/UPDATE).
func (g *AIClient) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (TaskTransition, error) {
	if g == nil || g.transport == nil {
		return TaskTransition{}, fmt.Errorf("AI client not initialized")
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

	modelName := g.resolveModel(parsed, g.transition)
	req := LLMRequest{
		Model:       modelName,
		System:      rendered,
		Temperature: 0.1,
		MaxTokens:   1024,
		JSONMode:    true,
		Thinking:    g.resolveThinking(parsed, g.transition),
	}
	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 30*time.Second, 2)
	if err != nil {
		return TaskTransition{}, err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-EvaluateTransition", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "EvaluateTransition", modelName, "", 0, resp.Usage)

	var result TaskTransition
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &result); err != nil {
		return TaskTransition{}, fmt.Errorf("failed to parse AI transition response: %w (raw: %s)", err, resp.Text)
	}

	return result, nil
}

// GenerateVisualizationData extracts graph structural data as JSON.
// Why: [Hallucination Defense] DeepSeek supports response_format json_object only (no
// json_schema), so the schema is described in the prompt and enforced via JSON mode +
// SanitizeJSON rather than the provider-native ResponseSchema.
func (g *AIClient) GenerateVisualizationData(ctx context.Context, email string, tasks string, reportID store.ReportID) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client not initialized")
	}

	prompt := fmt.Sprintf(`Extract a task network graph (Nodes=People, Links=Handover/Mention) from these logs.
Return ONLY a JSON object with this exact shape (no markdown, no commentary):
{
  "nodes": [{"id": "string", "name": "string", "value": number, "category": "string"}],
  "links": [{"source": "string", "target": "string", "weight": number}]
}
Logs:
%s`, tasks)

	req := LLMRequest{
		Model:       g.viz.model,
		System:      "Generate a JSON graph of task relations.",
		User:        prompt,
		Temperature: 0.0,
		MaxTokens:   4096,
		JSONMode:    true,
		Thinking:    g.viz.thinking,
	}
	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 60*time.Second, 2)
	if err != nil {
		return "", err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-Visualization", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "ReportVizData", g.viz.model, "", reportID, resp.Usage)
	return core.SanitizeJSON(resp.Text), nil
}

// GenerateMergedTaskTitle summarizes multiple task titles and messages into a single English title.
// Why: [Unified Consistency] Strictly enforces 30-character English limit via AI for unified task presentation.
func (g *AIClient) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client is not initialized")
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

	modelName := g.resolveModel(parsed, g.merge)
	req := LLMRequest{
		Model:       modelName,
		System:      rendered,
		Temperature: 0.1,
		MaxTokens:   100,
		Thinking:    g.resolveThinking(parsed, g.merge),
	}

	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 10*time.Second, 1)
	if err != nil {
		return "", err
	}

	logTokenUsage(ctx, email, "MergeSummary", modelName, "", 0, resp.Usage)
	_ = trace.Step(ctx, g.tracePrefix+"-MergeSummary", "", int(time.Since(start).Milliseconds()), 0)
	return core.CleanMarkdownText(resp.Text), nil
}
