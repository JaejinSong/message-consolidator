package ai

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// transitionFakeTransport records the request and replays one scripted response, so a test
// can assert both what EvaluateTaskTransition asked the provider for and how it reacts to a
// truncated body.
type transitionFakeTransport struct {
	mu       sync.Mutex
	reply    LLMResponse
	requests []LLMRequest
}

func (f *transitionFakeTransport) Generate(_ context.Context, req LLMRequest, _ time.Duration, _ int) (LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return f.reply, nil
}

func transitionTestClient(f *transitionFakeTransport) *AIClient {
	return &AIClient{
		provider:    providerDeepSeek,
		transport:   f,
		tracePrefix: "DeepSeek",
		transition:  modelSpec{"ds-default", ThinkOn},
	}
}

// TestEvaluateTransitionBudgetLeavesRoomForReasoning pins the output budget of the
// completion-detection call. Why: the stage runs with thinking on and Ollama bills reasoning
// against the same completion budget, so the verdict body competes with the reasoning trace.
// Measured on the completion_check prompt with a multi-subtask mixed-language reply,
// reasoning+answer spans 915-1404 tokens; the previous 1024 cap truncated 3/3 runs to an
// empty body on both deepseek-v4-flash:0731 and v4.1-flash, which silently dropped the
// completion detection. Shrinking this back below the reasoning range reintroduces that.
func TestEvaluateTransitionBudgetLeavesRoomForReasoning(t *testing.T) {
	t.Parallel()
	f := &transitionFakeTransport{reply: LLMResponse{Text: `{"status":"RESOLVE"}`}}
	if _, err := transitionTestClient(f).EvaluateTaskTransition(
		context.Background(), "me@example.com", "parent task", "already up on 8080", nil); err != nil {
		t.Fatalf("EvaluateTaskTransition: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(f.requests))
	}
	req := f.requests[0]
	if req.MaxTokens != DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want DefaultMaxTokens (%d) so reasoning cannot starve the verdict", req.MaxTokens, DefaultMaxTokens)
	}
	// Why: the budget only matters while reasoning is on -- if this ever flips to off the
	// measurement above no longer applies and the cap should be revisited deliberately.
	if req.Thinking != ThinkOn {
		t.Errorf("Thinking = %v, want ThinkOn (completion_check.prompt declares deepseekThinking: on)", req.Thinking)
	}
	if !req.JSONMode {
		t.Error("JSONMode = false, want true")
	}
}

// TestEvaluateTransitionReportsTruncationNotBadJSON guards the diagnosis, not just the cap.
// Why: a truncated body is unparseable, so before the FinishReason branch existed this
// surfaced as "failed to parse AI transition response: unexpected end of JSON input (raw: )"
// and read like a prompt or model defect rather than an output-budget overrun.
func TestEvaluateTransitionReportsTruncationNotBadJSON(t *testing.T) {
	t.Parallel()
	f := &transitionFakeTransport{reply: LLMResponse{
		Text:         "",
		FinishReason: "length",
		Usage:        LLMUsage{PromptTokens: 871, CompletionTokens: DefaultMaxTokens},
	}}
	_, err := transitionTestClient(f).EvaluateTaskTransition(
		context.Background(), "me@example.com", "parent task", "long mixed-language reply", nil)
	if err == nil {
		t.Fatal("err = nil, want a truncation error")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("err = %q, want it to name truncation", err)
	}
	if strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("err = %q, want the budget cause, not a JSON parse failure", err)
	}
}

// TestEvaluateTransitionParsesNormalResponse keeps the happy path honest: the truncation
// branch must not intercept a complete response that merely used a lot of the budget.
func TestEvaluateTransitionParsesNormalResponse(t *testing.T) {
	t.Parallel()
	f := &transitionFakeTransport{reply: LLMResponse{
		Text:  `{"status":"UPDATE","updated_text":"port confirmed 8080","subtask_updates":[{"index":0,"done":true}]}`,
		Usage: LLMUsage{CompletionTokens: 1404},
	}}
	res, err := transitionTestClient(f).EvaluateTaskTransition(
		context.Background(), "me@example.com", "parent task", "reply", nil)
	if err != nil {
		t.Fatalf("EvaluateTaskTransition: %v", err)
	}
	if res.Status != "UPDATE" {
		t.Errorf("Status = %q, want UPDATE", res.Status)
	}
	if res.UpdatedText != "port confirmed 8080" {
		t.Errorf("UpdatedText = %q, want the parsed body", res.UpdatedText)
	}
}
