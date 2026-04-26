package ai

import (
	"context"
	"fmt"
	"message-consolidator/store"
	"strings"
)

// GeminiLiteFilter handles high-speed noise filtering using Gemini 3.1 Flash Lite.
// This service offloads simple noise (greetings, system alerts) from the main extraction pipeline.
type GeminiLiteFilter struct {
	client *GeminiClient
}

func NewGeminiLiteFilter(client *GeminiClient) *GeminiLiteFilter {
	return &GeminiLiteFilter{client: client}
}

// IsNoise determines if a message is irrelevant/noise and should be skipped for extraction.
// Returns true if the message is noise, false if it contains actionable context.
// Why: [Performance] Filter logic is non-blocking and uses a cheaper model (Flash Lite) to save costs.
// `source` (slack|whatsapp|telegram|gmail|...) attributes the token cost to the right bucket.
func (f *GeminiLiteFilter) IsNoise(ctx context.Context, email, source, text string) (bool, error) {
	prompt := LoadPrompt("lite_filter.prompt")
	data := ExtractionContext{
		MessagePayload: text,
		CurrentUser:    email,
	}

	rendered, err := prompt.Render(data)
	if err != nil {
		return false, fmt.Errorf("filter prompt render error: %w", err)
	}

	// Use binary filter model (Flash Lite)
	result, err := f.client.CallGenericAPI(ctx, email, "LiteFilter", source, prompt.Meta.Model, rendered)
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
