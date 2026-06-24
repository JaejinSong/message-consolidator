package ai

import (
	"context"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/store"
	"strings"
)

// GeminiLiteFilter handles high-speed noise filtering using the configured filter model.
// This service offloads simple noise (greetings, system alerts) from the main extraction pipeline.
type GeminiLiteFilter struct {
	client *AIClient
}

func NewGeminiLiteFilter(client *AIClient) *GeminiLiteFilter {
	return &GeminiLiteFilter{client: client}
}

// IsNoise determines if a message is irrelevant/noise and should be skipped for extraction.
// Returns true if the message is noise, false if it contains actionable context.
// Why: [Performance] Filter logic is non-blocking and uses a cheaper model to save costs.
// `source` (slack|whatsapp|telegram|gmail|...) attributes the token cost to the right bucket.
func (f *GeminiLiteFilter) IsNoise(ctx context.Context, email, source, text string) (bool, error) {
	data := core.ExtractionContext{
		MessagePayload: text,
		CurrentUser:    email,
	}

	// Why: the static filter instruction lives in the system role and the per-message
	// context in the user role so the system prefix stays cacheable across calls.
	systemPrompt := core.LoadPrompt(core.PromptLiteFilter)
	system, err := systemPrompt.Render(data)
	if err != nil {
		return false, fmt.Errorf("filter system prompt render error: %w", err)
	}
	user, err := core.LoadPrompt(core.PromptLiteFilterUser).Render(data)
	if err != nil {
		return false, fmt.Errorf("filter user prompt render error: %w", err)
	}

	modelName := f.client.resolveModel(systemPrompt, f.client.filter)
	result, err := f.client.CallGenericAPI(ctx, email, "LiteFilter", source, modelName, system, user)
	if err != nil {
		return false, fmt.Errorf("filter execution error: %w", err)
	}

	// Why: lite_filter.prompt outputs TRUE=actionable, FALSE=noise.
	isNoise := strings.TrimSpace(strings.ToUpper(result)) == "FALSE"
	if isNoise {
		store.IncrementFilteredCount(email)
	}

	return isNoise, nil
}
