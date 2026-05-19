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
	"google.golang.org/genai"
)

func (g *GeminiClient) Translate(ctx context.Context, email string, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.client == nil || len(tasks) == 0 {
		return nil, fmt.Errorf("invalid translate request")
	}

	cfg, modelName, prompt := g.prepareTranslateResources(language, tasks)
	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(prompt), cfg, 30*time.Second, 2)
	if err != nil {
		return nil, err
	}

	_ = trace.Step(ctx, "Gemini-Translate", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Translate", modelName, "", 0, resp)
	return g.parseTranslateResults(resp)
}

func (g *GeminiClient) prepareTranslateResources(lang string, requests []store.TranslateRequest) (*genai.GenerateContentConfig, string, string) {
	parsed := core.LoadPrompt(core.PromptTranslationSystem)
	sysInst, _ := parsed.Render(core.ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	modelName := g.getEffectiveModel(parsed, g.translationModel)
	cfg := g.buildConfig(0.0, 4096, "application/json", sysInst)
	tasksJSON, _ := json.Marshal(requests)
	return cfg, modelName, string(tasksJSON)
}

func (g *GeminiClient) parseTranslateResults(resp *genai.GenerateContentResponse) ([]store.TranslateRequest, error) {
	raw, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}
	return core.UnmarshalTranslate(core.SanitizeJSON(raw), raw, "")
}

// Why: Translates a complete Markdown report into a target language while strictly preserving the structure.
// Uses the lightweight Flash-Lite model for maximum cost efficiency.
func (g *GeminiClient) TranslateReport(ctx context.Context, email string, reportInEnglish string, targetLanguage string, reportID store.ReportID) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptReportTranslator)
	data := core.ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	modelName := g.getEffectiveModel(parsed, g.translationModel)
	cfg := g.buildConfig(0.2, ReportMaxTokens, "", sysInst)

	logger.Debugf("[GEMINI] Translating Markdown report for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(reportInEnglish), cfg, 45*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Report translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	_ = trace.Step(ctx, "Gemini-TranslateReport", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateReport", modelName, "", reportID, resp)
	return extractResponseText(resp)
}

// TranslateTaskMessage translates short, conversational messages (Slack, WhatsApp, Email).
// It use a specialized prompt to maintain tone and prevent unnecessary formatting (e.g., markdown bloat).
func (g *GeminiClient) TranslateTaskMessage(ctx context.Context, email string, text string, targetLanguage string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := core.LoadPrompt(core.PromptTaskTranslator)
	data := core.ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	modelName := g.getEffectiveModel(parsed, g.translationModel)
	cfg := g.buildConfig(0.1, 0, "", sysInst)

	logger.Debugf("[GEMINI] Translating Task for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(text), cfg, 30*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Task translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	_ = trace.Step(ctx, "Gemini-TranslateTask", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateTask", modelName, "", 0, resp)
	return extractResponseText(resp)
}

// TranslateTasksBatch translates multiple tasks at once following the Page-unit Pure JIT pattern.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt with a 25-item threshold.
func (g *GeminiClient) TranslateTasksBatch(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]TranslationResult, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	if len(tasks) > 25 {
		return g.translateInChunks(ctx, email, tasks, lang, 25)
	}

	parsed := core.LoadPrompt(core.PromptBatchTranslator)
	data := core.ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	modelName := g.getEffectiveModel(parsed, g.translationModel)
	cfg := g.buildConfig(0.1, DefaultMaxTokens, "application/json", sysInst)

	tasksJSON, _ := json.Marshal(tasks)
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(string(tasksJSON)), cfg, 45*time.Second, 3)
	if err != nil {
		// Why: Mirror ReportSummary failure attribution — surface burned-but-unattributed
		// retry-exhausted calls so the cost dashboard can flag invisible spend.
		// Gemini does not return UsageMetadata on timeout/cancel.
		if uErr := store.AddTokenUsage(email, "BatchTranslate", modelName, "failed", 0, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] BatchTranslate failure attribution: %v", uErr)
		}
		return nil, err
	}

	logTokenUsage(ctx, email, "BatchTranslate", modelName, "", 0, resp)
	raw, _ := extractResponseText(resp)
	var results []TranslationResult
	if err := json.Unmarshal([]byte(core.SanitizeJSON(raw)), &results); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return results, nil
}

func (g *GeminiClient) translateInChunks(ctx context.Context, email string, tasks []store.TranslateRequest, lang string, chunkSize int) ([]TranslationResult, error) {
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
