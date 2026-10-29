package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/logger"
	"message-consolidator/store"
	"time"

	"github.com/whatap/go-api/trace"
)

func (g *AIClient) Translate(ctx context.Context, email string, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.transport == nil || len(tasks) == 0 {
		return nil, fmt.Errorf("invalid translate request")
	}

	parsed := core.LoadPrompt(core.PromptTranslationSystem)
	sysInst, _ := parsed.Render(core.ExtractionContext{
		Locale:      g.getValidLang(language),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	modelName := g.resolveModel(parsed, g.translate)
	tasksJSON, _ := json.Marshal(tasks)

	req := LLMRequest{
		Model:       modelName,
		System:      sysInst,
		User:        string(tasksJSON),
		Temperature: 0.0,
		MaxTokens:   4096,
		JSONMode:    true,
		Thinking:    g.resolveThinking(parsed, g.translate),
	}
	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 30*time.Second, 2)
	if err != nil {
		return nil, err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-Translate", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Translate", modelName, "", 0, resp.Usage)
	return core.UnmarshalTranslate(core.SanitizeJSON(resp.Text), resp.Text, "")
}

// Why: Translates a complete Markdown report into a target language while strictly preserving the structure.
// Uses the lightweight translation model for maximum cost efficiency.
func (g *AIClient) TranslateReport(ctx context.Context, email string, reportInEnglish string, targetLanguage string, reportID store.ReportID) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptReportTranslator)
	sysInst, _ := parsed.Render(core.ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	modelName := g.resolveModel(parsed, g.translate)
	req := LLMRequest{
		Model:       modelName,
		System:      sysInst,
		User:        reportInEnglish,
		Temperature: 0.2,
		MaxTokens:   ReportMaxTokens,
		Thinking:    g.resolveThinking(parsed, g.translate),
	}

	logger.Debugf("[AI] Translating Markdown report for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 45*time.Second, 2)
	if err != nil {
		logger.Errorf("[AI] Report translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-TranslateReport", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateReport", modelName, "", reportID, resp.Usage)
	return resp.Text, nil
}

// TranslateTaskMessage translates short, conversational messages (Slack, WhatsApp, Email).
// It uses a specialized prompt to maintain tone and prevent unnecessary formatting (e.g., markdown bloat).
func (g *AIClient) TranslateTaskMessage(ctx context.Context, email string, text string, targetLanguage string) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptTaskTranslator)
	sysInst, _ := parsed.Render(core.ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	modelName := g.resolveModel(parsed, g.translate)
	req := LLMRequest{
		Model:       modelName,
		System:      sysInst,
		User:        text,
		Temperature: 0.1,
		Thinking:    g.resolveThinking(parsed, g.translate),
	}

	logger.Debugf("[AI] Translating Task for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 30*time.Second, 2)
	if err != nil {
		logger.Errorf("[AI] Task translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-TranslateTask", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateTask", modelName, "", 0, resp.Usage)
	return resp.Text, nil
}

// TranslateTasksBatch translates multiple tasks at once following the Page-unit Pure JIT pattern.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt with a 25-item threshold.
func (g *AIClient) TranslateTasksBatch(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]TranslationResult, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	if len(tasks) > 25 {
		return g.translateInChunks(ctx, email, tasks, lang, 25)
	}

	parsed := core.LoadPrompt(core.PromptBatchTranslator)
	sysInst, _ := parsed.Render(core.ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	modelName := g.resolveModel(parsed, g.translate)
	tasksJSON, _ := json.Marshal(tasks)

	req := LLMRequest{
		Model:       modelName,
		System:      sysInst,
		User:        string(tasksJSON),
		Temperature: 0.1,
		MaxTokens:   DefaultMaxTokens,
		JSONMode:    true,
		Thinking:    g.resolveThinking(parsed, g.translate),
	}
	resp, err := g.transport.Generate(ctx, req, 45*time.Second, 3)
	if err != nil {
		// Why: Mirror ReportSummary failure attribution — surface burned-but-unattributed
		// retry-exhausted calls so the cost dashboard can flag invisible spend.
		if uErr := store.AddTokenUsage(email, "BatchTranslate", modelName, "failed", 0, 0, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] BatchTranslate failure attribution: %v", uErr)
		}
		return nil, err
	}

	logTokenUsage(ctx, email, "BatchTranslate", modelName, "", 0, resp.Usage)
	var results []TranslationResult
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &results); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return results, nil
}

func (g *AIClient) translateInChunks(ctx context.Context, email string, tasks []store.TranslateRequest, lang string, chunkSize int) ([]TranslationResult, error) {
	var allResults []TranslationResult
	for i := 0; i < len(tasks); i += chunkSize {
		end := i + chunkSize
		if end > len(tasks) {
			end = len(tasks)
		}

		chunk, err := g.TranslateTasksBatch(ctx, email, tasks[i:end], lang)
		if err != nil {
			// Why: Return partial successes alongside the error so a single chunk failure
			// doesn't waste tokens already burned on prior chunks. logTokenUsage has already
			// attributed those chunks; callers SaveTaskTranslationsBulk-cache the partial map.
			return allResults, fmt.Errorf("chunk %d-%d failed (kept %d prior results): %w", i, end, len(allResults), err)
		}
		allResults = append(allResults, chunk...)
	}
	return allResults, nil
}
