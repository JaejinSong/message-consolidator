//go:build deepseek_live

// Live integration tests against the real DeepSeek API. Excluded from the default
// build/CI by the deepseek_live tag and additionally skipped when DEEPSEEK_API_KEY
// is unset, so they never make network calls or incur cost unless run explicitly:
//
//	DEEPSEEK_API_KEY=sk-... go test -tags=deepseek_live ./ai/ -run TestLive -v
package ai

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func liveTransport(t *testing.T) *deepseekTransport {
	t.Helper()
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live DeepSeek test")
	}
	tr, err := newDeepSeekTransport(key, os.Getenv("DEEPSEEK_BASE_URL"))
	if err != nil {
		t.Fatalf("newDeepSeekTransport: %v", err)
	}
	return tr
}

func liveClient(t *testing.T) *AIClient {
	t.Helper()
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live DeepSeek test")
	}
	c, err := NewAIClient(context.Background(), ProviderConfig{
		Provider:        "deepseek",
		DeepSeekAPIKey:  key,
		DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
	})
	if err != nil {
		t.Fatalf("NewAIClient(deepseek): %v", err)
	}
	return c
}

// TestLive_DeepSeek_BasicGeneration verifies auth, connectivity and usage accounting.
func TestLive_DeepSeek_BasicGeneration(t *testing.T) {
	tr := liveTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := tr.Generate(ctx, LLMRequest{
		Model:       deepSeekFlashModel,
		System:      "You are a terse assistant.",
		User:        "Reply with the single word: pong",
		Temperature: 0.0,
		MaxTokens:   64,
	}, 30*time.Second, 1)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.TrimSpace(resp.Text) == "" {
		t.Error("expected non-empty text")
	}
	if resp.Usage.PromptTokens <= 0 || resp.Usage.CompletionTokens <= 0 {
		t.Errorf("expected positive usage, got prompt=%d completion=%d", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	t.Logf("text=%q usage=%+v finish=%q", resp.Text, resp.Usage, resp.FinishReason)
}

// TestLive_DeepSeek_JSONMode verifies response_format json_object yields parseable JSON.
func TestLive_DeepSeek_JSONMode(t *testing.T) {
	tr := liveTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := tr.Generate(ctx, LLMRequest{
		Model:       deepSeekFlashModel,
		System:      "Output only a JSON object.",
		User:        `Return a JSON object with keys "a" (integer 7) and "b" (string "hello").`,
		Temperature: 0.0,
		MaxTokens:   128,
		JSONMode:    true,
	}, 30*time.Second, 1)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Text), &got); err != nil {
		t.Fatalf("json mode returned non-JSON %q: %v", resp.Text, err)
	}
	if _, ok := got["a"]; !ok {
		t.Errorf("expected key 'a' in %v", got)
	}
	if _, ok := got["b"]; !ok {
		t.Errorf("expected key 'b' in %v", got)
	}
	t.Logf("json=%v usage=%+v", got, resp.Usage)
}

// TestLive_DeepSeek_ReasonerThinking verifies ThinkOn (reasoning_effort) yields a
// correct reasoned answer. NOTE: Ollama returns the reasoning text in message.reasoning
// (not reasoning_content) and omits completion_tokens_details, so ReasoningTokens is
// always 0 here — reasoning tokens are folded into completion_tokens (same billing rate).
func TestLive_DeepSeek_ReasonerThinking(t *testing.T) {
	tr := liveTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := tr.Generate(ctx, LLMRequest{
		Model:     deepSeekFlashModel,
		Thinking:  ThinkOn,
		User:      "A bat and ball cost $1.10. The bat costs $1.00 more than the ball. How much is the ball? Answer with just the amount.",
		MaxTokens: 1024,
	}, 90*time.Second, 1)
	if err != nil {
		t.Fatalf("Generate(reasoner): %v", err)
	}
	if !strings.Contains(resp.Text, "0.05") {
		t.Errorf("expected reasoned answer $0.05, got %q", resp.Text)
	}
	t.Logf("text=%q usage=%+v", resp.Text, resp.Usage)
}

// TestLive_DeepSeek_EvaluateTransition exercises the production transition path
// (ThinkOn + JSON mode) end-to-end through AIClient.
func TestLive_DeepSeek_EvaluateTransition(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	res, err := c.EvaluateTaskTransition(ctx, "live@test",
		"Send the Q2 report to the finance team",
		"Done, I just emailed the Q2 report to finance.", nil)
	if err != nil {
		t.Fatalf("EvaluateTaskTransition: %v", err)
	}
	if strings.TrimSpace(res.Status) == "" {
		t.Errorf("expected a non-empty transition status, got %+v", res)
	}
	t.Logf("transition=%+v", res)
}

// TestLive_DeepSeek_CompletionCheckRegression exercises the v2.6.0 completion_check rules
// end-to-end against the real model. Why: the fake-transport tests in
// transition_budget_test.go only pin the request shape (MaxTokens/Thinking/JSONMode) --
// they replay a scripted reply and cannot catch a classification regression in the prompt
// itself. This runs each case 3x and reports the pass count, matching the production
// examples from the 2026-09-27 dry-run plus the positive/future-tense cases the fix must
// keep or add.
func TestLive_DeepSeek_CompletionCheckRegression(t *testing.T) {
	c := liveClient(t)

	cases := []struct {
		name       string
		parentTask string
		reply      string
		want       string
	}{
		{
			name:       "volunteer_substitute_resolves",
			parentTask: "Attend weekly report meeting as substitute",
			reply:      "주간보고 대타 참석 하겠습니다.",
			want:       "RESOLVE",
		},
		{
			name:       "volunteer_cover_resolves",
			parentTask: "Find someone to cover the Friday customer demo",
			reply:      "I'll cover this.",
			want:       "RESOLVE",
		},
		{
			name:       "receipt_ack_not_completion",
			parentTask: "Fill in the security form",
			reply:      "ok, noted",
			want:       "NONE",
		},
		{
			name:       "future_tense_promise_not_completion",
			parentTask: "Obtain the Notion Internal Integration token",
			reply:      "Hmm.. Ok. I'll check and update you.",
			want:       "NONE",
		},
		{
			name:       "requester_thanks_after_delivery_resolves",
			parentTask: "Obtain the Notion Internal Integration token",
			reply:      "thanks JJ",
			want:       "RESOLVE",
		},
		{
			name:       "explicit_done_resolves",
			parentTask: "Send the Q2 report to the finance team",
			reply:      "Done, finished this already.",
			want:       "RESOLVE",
		},
		{
			name:       "sent_the_file_resolves",
			parentTask: "Send the signed contract to legal",
			reply:      "Sent the file to legal just now.",
			want:       "RESOLVE",
		},
		{
			name:       "requester_thanks_received_resolves",
			parentTask: "Share the deployment checklist",
			reply:      "Thanks, got it, received.",
			want:       "RESOLVE",
		},
		{
			name:       "future_tense_indonesian_not_completion",
			parentTask: "Update the vendor contract terms",
			reply:      "Baik, akan saya cek nanti saya update.",
			want:       "NONE",
		},
	}

	const runsPerCase = 3
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			passed := 0
			for i := 0; i < runsPerCase; i++ {
				res, err := c.EvaluateTaskTransition(ctx, "live@test", tc.parentTask, tc.reply, nil)
				if err != nil {
					t.Errorf("run %d: EvaluateTaskTransition: %v", i, err)
					continue
				}
				if res.Status == tc.want {
					passed++
				} else {
					t.Logf("run %d: status=%q, want %q (reply=%q)", i, res.Status, tc.want, tc.reply)
				}
			}
			t.Logf("%s: %d/%d runs matched %q", tc.name, passed, runsPerCase, tc.want)
			if passed == 0 {
				t.Errorf("%s: 0/%d runs matched %q", tc.name, runsPerCase, tc.want)
			}
		})
	}
}
