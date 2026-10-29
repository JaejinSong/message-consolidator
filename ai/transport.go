package ai

import (
	"context"
	"time"
)

// ThinkingMode is a provider-neutral toggle for model "thinking"/reasoning.
// Why: "unset" (model default) is distinct from "explicitly disabled"; collapsing them
// would change extraction behavior, so ThinkDefault leaves the provider default untouched.
// DeepSeek (via Ollama) maps this onto reasoning_effort — see reasoningEffort.
type ThinkingMode int

const (
	ThinkDefault ThinkingMode = iota // provider default
	ThinkOff                         // explicitly disable
	ThinkOn                          // enable
	ThinkHigh                        // maximum reasoning (Ollama: reasoning_effort "high")
)

// LLMRequest is the provider-neutral generation request shared by every AI step.
// System/User mirror the OpenAI role split.
type LLMRequest struct {
	Model       string
	System      string
	User        string
	Temperature float64
	MaxTokens   int
	JSONMode    bool
	Thinking    ThinkingMode
	// Seed pins sampling where the provider supports it (Ollama's OpenAI-compatible API
	// does). Zero means unset. Why: the BLUF stage samples the same
	// prompt several times, so its regression harness needs one knob to make a run
	// repeatable without collapsing the deliberate temperature spread.
	Seed int
}

// LLMUsage normalizes token accounting. ReasoningTokens maps
// DeepSeek completion_tokens_details.reasoning_tokens.
// CachedTokens maps prompt-cache hits (DeepSeek prompt_tokens_details.cached_tokens);
// captured but not yet persisted — see the token_usage cached_tokens follow-up.
type LLMUsage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
}

// LLMResponse is the provider-neutral generation result. FinishReason is
// normalized to "" (normal) | "length" (output truncated, OpenAI "length")
// so callers can trigger truncation repair provider-agnostically.
type LLMResponse struct {
	Text         string
	FinishReason string
	Usage        LLMUsage
}

// LLMTransport is the single provider seam: one method, implemented by
// deepseekTransport. All prompt building, response parsing and token logging
// lives above it in AIClient.
type LLMTransport interface {
	Generate(ctx context.Context, req LLMRequest, timeout time.Duration, maxRetries int) (LLMResponse, error)
}
