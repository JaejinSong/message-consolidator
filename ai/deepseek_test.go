package ai

import (
	"context"
	"encoding/json"
	"io"
	"message-consolidator/ai/core"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// newMockDeepSeek spins up an OpenAI-compatible stub and returns a transport pointed at it
// plus a pointer that captures the decoded request for assertions.
func newMockDeepSeek(t *testing.T, resp openai.ChatCompletionResponse) (*deepseekTransport, *openai.ChatCompletionRequest) {
	t.Helper()
	captured := &openai.ChatCompletionRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	tr, err := newDeepSeekTransport("test-key", srv.URL)
	if err != nil {
		t.Fatalf("newDeepSeekTransport: %v", err)
	}
	return tr, captured
}

func TestDeepSeekTransportGenerate(t *testing.T) {
	t.Parallel()
	mockResp := openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: "assistant", Content: `{"ok":true}`, ReasoningContent: "thinking..."},
			FinishReason: openai.FinishReasonLength,
		}},
		Usage: openai.Usage{
			PromptTokens:            100,
			CompletionTokens:        40,
			CompletionTokensDetails: &openai.CompletionTokensDetails{ReasoningTokens: 12},
			PromptTokensDetails:     &openai.PromptTokensDetails{CachedTokens: 30},
		},
	}
	tr, got := newMockDeepSeek(t, mockResp)

	req := LLMRequest{Model: "deepseek-v4-flash:0731", System: "sys", User: "usr", Temperature: 0.2, MaxTokens: 256, JSONMode: true, Thinking: ThinkOff}
	resp, err := tr.Generate(context.Background(), req, 5*time.Second, 0)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Response mapping.
	if resp.Text != `{"ok":true}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want length", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 100 || resp.Usage.CompletionTokens != 40 {
		t.Errorf("Usage prompt/completion = %d/%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	if resp.Usage.ReasoningTokens != 12 {
		t.Errorf("ReasoningTokens = %d, want 12", resp.Usage.ReasoningTokens)
	}
	if resp.Usage.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", resp.Usage.CachedTokens)
	}

	// Request mapping.
	if got.Model != "deepseek-v4-flash:0731" {
		t.Errorf("model = %q", got.Model)
	}
	if got.ReasoningEffort != "none" {
		t.Errorf("reasoning_effort = %q, want none for ThinkOff", got.ReasoningEffort)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Role != openai.ChatMessageRoleSystem || got.Messages[0].Content != "sys" {
		t.Errorf("system message = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != openai.ChatMessageRoleUser || got.Messages[1].Content != "usr" {
		t.Errorf("user message = %+v", got.Messages[1])
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONObject {
		t.Errorf("response_format = %+v, want json_object", got.ResponseFormat)
	}
	if got.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", got.MaxTokens)
	}
}

func TestDeepSeekTransportNonJSONAndEmptyUser(t *testing.T) {
	t.Parallel()
	mockResp := openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Content: "plain text"},
			FinishReason: openai.FinishReasonStop,
		}},
	}
	tr, got := newMockDeepSeek(t, mockResp)

	// System-only prompt (User empty) must not produce json mode and must placeholder the user message.
	req := LLMRequest{Model: "deepseek-v4-pro", System: "instructions", JSONMode: false}
	resp, err := tr.Generate(context.Background(), req, 5*time.Second, 0)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.FinishReason != "" {
		t.Errorf("FinishReason = %q, want empty for stop", resp.FinishReason)
	}
	if got.ResponseFormat != nil {
		t.Errorf("response_format should be nil when JSONMode is false, got %+v", got.ResponseFormat)
	}
	if len(got.Messages) != 2 || got.Messages[1].Content != "." {
		t.Errorf("empty user must be placeholdered with '.', got messages=%+v", got.Messages)
	}
	if got.ReasoningEffort != "" {
		t.Errorf("reasoning_effort = %q, want omitted for ThinkDefault", got.ReasoningEffort)
	}
}

func TestReasoningEffortMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode ThinkingMode
		want string
	}{
		{ThinkOn, "medium"},
		{ThinkOff, "none"},
		{ThinkDefault, ""},
	}
	for _, tc := range cases {
		if got := reasoningEffort(tc.mode); got != tc.want {
			t.Errorf("reasoningEffort(%v) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestDeepSeekTransportMissingKey(t *testing.T) {
	t.Parallel()
	if _, err := newDeepSeekTransport("", ""); err == nil {
		t.Error("expected error for empty DEEPSEEK_API_KEY")
	}
}

func TestNewAIClientDeepSeekStages(t *testing.T) {
	t.Parallel()
	c, err := NewAIClient(context.Background(), ProviderConfig{Provider: "deepseek", DeepSeekAPIKey: "k"})
	if err != nil {
		t.Fatalf("NewAIClient(deepseek): %v", err)
	}
	if c.provider != providerDeepSeek || c.tracePrefix != "DeepSeek" {
		t.Errorf("provider/prefix = %q/%q", c.provider, c.tracePrefix)
	}
	if c.analyze.model != deepSeekFlashModel || c.analyze.thinking != ThinkOff {
		t.Errorf("analyze spec = %+v", c.analyze)
	}
	if c.report.model != deepSeekProModel || c.report.thinking != ThinkOn {
		t.Errorf("report spec = %+v", c.report)
	}
	if c.transition.model != deepSeekFlashModel || c.transition.thinking != ThinkOn {
		t.Errorf("transition spec = %+v", c.transition)
	}
	if c.identity.model != deepSeekFlashModel {
		t.Errorf("identity model = %q", c.identity.model)
	}
}

func TestResolveModelProviderRouting(t *testing.T) {
	t.Parallel()
	p := &core.ParsedPrompt{Meta: core.PromptMeta{
		GeminiModel: "gemini-3-flash-preview", GeminiThinking: "off",
		DeepSeekModel: deepSeekProModel, DeepSeekThinking: "on",
	}}

	gem := &AIClient{provider: providerGemini, report: modelSpec{model: "gemini-default", thinking: ThinkDefault}}
	if got := gem.resolveModel(p, gem.report); got != "gemini-3-flash-preview" {
		t.Errorf("Gemini must use geminiModel, got %q", got)
	}
	if got := gem.resolveThinking(p, gem.report); got != ThinkOff {
		t.Errorf("Gemini must use geminiThinking=off, got %v", got)
	}
	if got := gem.resolveModel(nil, gem.report); got != "gemini-default" {
		t.Errorf("nil prompt must fall back to spec model, got %q", got)
	}

	ds := &AIClient{provider: providerDeepSeek, report: modelSpec{model: "ds-default", thinking: ThinkOff}}
	if got := ds.resolveModel(p, ds.report); got != deepSeekProModel {
		t.Errorf("DeepSeek must use deepseekModel, got %q", got)
	}
	if got := ds.resolveThinking(p, ds.report); got != ThinkOn {
		t.Errorf("DeepSeek must use deepseekThinking=on, got %v", got)
	}
	if got := ds.resolveModel(&core.ParsedPrompt{}, ds.report); got != "ds-default" {
		t.Errorf("omitted deepseekModel must fall back to spec, got %q", got)
	}
}

func TestDeepSeekUsageMapping(t *testing.T) {
	t.Parallel()
	// Nil detail pointers must not panic and yield zero reasoning/cached.
	u := deepSeekUsage(openai.Usage{PromptTokens: 5, CompletionTokens: 7})
	if u.PromptTokens != 5 || u.CompletionTokens != 7 || u.ReasoningTokens != 0 || u.CachedTokens != 0 {
		t.Errorf("usage = %+v", u)
	}
}
