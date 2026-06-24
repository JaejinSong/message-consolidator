//go:build deepseek_live

// Live DeepSeek prompt-cache guard for the Step 13 prefix-stabilization work.
// Excluded from default build/CI by the deepseek_live tag and skipped without
// DEEPSEEK_API_KEY. Run explicitly:
//
//	DEEPSEEK_API_KEY=sk-... go test -tags=deepseek_live ./ai/ -run TestLive_DeepSeek_CacheHit -v
//
// Invariant: the chat extraction system prompt must be byte-identical across
// different messages so DeepSeek's prefix cache reuses it on every scan (cache-hit
// input is 1/50 the price of a miss). Measured before/after for two different
// same-room messages (2382-token system, ~2975-token request):
//
//	identical repeat (ceiling) : 98.9% cached
//	NEW layout (this code)     : 77.4% cached  (system byte-stable across msgs)
//	OLD layout (few-shots+time
//	            inside system) :  0%   cached  (system bytes differed per msg)
//
// If a future change re-introduces volatile content (few-shots, Current Time,
// per-message context) into the system prompt, GetSystemInstruction stops being
// byte-stable and this guard fails.
package ai

import (
	"context"
	"testing"
	"time"

	"message-consolidator/ai/core"
)

const sampleTasksJSON = `[{"id":101,"task":"Align POC scope via online session","original_text":"let's set up a call for the POC to align scope and timeline","requester":"Vy","assignee":"Vy","category":"PROMISE","done":false},` +
	`{"id":102,"task":"Track Srisawad NDA signing progress","original_text":"we are now waiting for the NDA signing, please review the NDA sent via email","assignee":"shared","category":"WAITING","done":false},` +
	`{"id":103,"task":"Check service version on on-premise collection server","original_text":"Adira want to know what service version currently running on their collection server","assignee":"shared","category":"QUERY","done":false}]`

func renderChat(payload string) (system, user string) {
	a := core.GetAnalyzer("slack")
	data := core.ExtractionContext{
		MessagePayload:    payload,
		CurrentTime:       time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:            "en-US",
		ExistingTasksJSON: sampleTasksJSON,
		CurrentUser:       "Jaejin Song",
		CurrentUserEmail:  "jjsong@whatap.io",
		ChatType:          "group",
		RoomName:          "TestRoom",
	}
	return a.GetSystemInstruction(data), a.GetUserPrompt(data)
}

func TestLive_DeepSeek_CacheHit(t *testing.T) {
	tr := liveTransport(t)

	msgA := "[ID:s1] Yosep: please deploy the new build to staging and check the dashboards before the demo"
	msgB := "[ID:s2] Manager: can you finish the tech blog post and update the onboarding guide by Friday"

	sysA, userA := renderChat(msgA)
	sysB, userB := renderChat(msgB)
	if sysA != sysB {
		t.Fatalf("chat system prompt is not byte-stable across messages — prefix cache broken")
	}

	gen := func(system, user string) LLMUsage {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		resp, err := tr.Generate(ctx, LLMRequest{
			Model: deepSeekChatModel, System: system, User: user,
			Temperature: 0.0, MaxTokens: DefaultMaxTokens, JSONMode: true,
		}, 45*time.Second, 1)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return resp.Usage
	}

	_ = gen(sysA, userA)            // warm the static system prefix
	time.Sleep(5 * time.Second)     // let the partial-prefix cache node commit
	usage := gen(sysB, userB)       // different same-room message

	hit := 0.0
	if usage.PromptTokens > 0 {
		hit = float64(usage.CachedTokens) / float64(usage.PromptTokens) * 100
	}
	t.Logf("different same-room message: prompt=%d cached=%d (%.1f%% hit)", usage.PromptTokens, usage.CachedTokens, hit)

	if usage.CachedTokens <= 0 {
		t.Errorf("expected the byte-stable system prefix to hit the cache on a different message, got %d cached", usage.CachedTokens)
	}
}
