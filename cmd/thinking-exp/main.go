// Thinking token experiment: isolates whether thinking scales with input size or prompt complexity.
// Run: go run ./cmd/thinking-exp/ from project root.
package main

import (
	"context"
	"fmt"
	"message-consolidator/ai/core"
	"os"
	"time"

	"google.golang.org/genai"
)

const maxTokens = 40960

// minimalTasks is ~3 tasks — extreme low end of real input.
const minimalTasks = `# Stats: 3 activity (2 active, 1 done) | 0 stalled
# Top open assignees: alice×2(100%)
# Room→Customer: biz-global-malaysia→Malaysia Biz
[Activity Tasks]
- [ ][TASK] POC setup for Malaysia Biz (Room: biz-global-malaysia, From: Alice (Internal), To: Bob (Internal), Age: 3wd) | Evidence: Need to confirm infra access before Monday.
- [ ][TASK] License renewal for Malaysia Biz (Room: biz-global-malaysia, From: Carol (External), To: Alice (Internal), Age: 1wd) | Evidence: Waiting on finance sign-off, deadline is tight.
- [V][TASK] Kickoff call scheduled (Room: biz-global-malaysia, From: Alice (Internal), To: Bob (Internal))`

// fullTasks simulates a real-sized report (~50 tasks, matching prompt_tokens ~4500).
// We reuse the minimal block repeated to hit the size without waiting for real data.
var fullTasks string

func init() {
	var sb string
	sb = `# Stats: 48 activity (32 active, 16 done) | 5 stalled
# Top open assignees: alice×12(38%), bob×10(31%), carol×6(19%)
# Room→Customer: biz-global-malaysia→Malaysia Biz, biz-global-indonesia→Indonesia Biz, Canadia-Whatap→Canadia
# Cross-source: "Canadia" in [Canadia-Whatap, biz-global-cambodia, WhaTap-Weefer]
[Activity Tasks]
`
	task := "- [ ][TASK] POC setup for Customer%d (Room: biz-global-malaysia, From: Alice (Internal), To: Bob (Internal), Age: %dwd) | Evidence: Need to confirm infra access. Scalability concern raised. [RISK-CAND]\n"
	taskDone := "- [V][TASK] License issued for Customer%d (Room: biz-global-indonesia, From: Carol (External), To: Alice (Internal))\n"
	for i := 1; i <= 32; i++ {
		sb += fmt.Sprintf(task, i, (i%7)+1)
	}
	for i := 1; i <= 16; i++ {
		sb += fmt.Sprintf(taskDone, i)
	}
	sb += "[Stalled Tasks - active items predating window]\n"
	for i := 1; i <= 5; i++ {
		sb += fmt.Sprintf("- [ ][TASK] Stalled task %d (Room: biz-global-malaysia, From: Alice (Internal), To: Bob (Internal), Age: %dwd)\n", i, 10+i*2)
	}
	fullTasks = sb
}

// simplePrompt is a stripped-down instruction with no grounding rules or examples.
const simplePrompt = `Write a short executive summary of the tasks below. Include: total count, top assignee, any risks.
Output format: plain text, 3-5 sentences.`

func run(ctx context.Context, client *genai.Client, label, sysPrompt, tasks string) {
	data := core.ExtractionContext{
		MessagePayload:   tasks,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		StaleThreshold:   5,
		CurrentUserEmail: "test@whatap.io",
	}

	var rendered string
	if sysPrompt == "" {
		parsed := core.LoadPrompt(core.PromptReportSummary)
		var err error
		rendered, err = parsed.Render(data)
		if err != nil {
			fmt.Printf("[%s] render error: %v\n", label, err)
			return
		}
	} else {
		rendered = sysPrompt + "\n\n" + tasks
	}

	cfg := &genai.GenerateContentConfig{
		MaxOutputTokens:   maxTokens,
		SystemInstruction: genai.NewContentFromText(rendered, ""),
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
		},
	}

	apiCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	start := time.Now()
	resp, err := client.Models.GenerateContent(apiCtx, "gemini-3.5-flash", genai.Text("."), cfg)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("[%s] ERROR: %v\n", label, err)
		return
	}

	u := resp.UsageMetadata
	fmt.Printf("[%s] thinking=%d  completion=%d  prompt=%d  total=%d  elapsed=%s\n",
		label, u.ThoughtsTokenCount, u.CandidatesTokenCount, u.PromptTokenCount,
		u.TotalTokenCount, elapsed)
}

func main() {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "GEMINI_API_KEY not set")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "client init: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Thinking Token Experiment ===")
	fmt.Println("Each call may take 60-180s. Running sequentially...")
	fmt.Println()

	// Exp 1: full prompt + minimal input (3 tasks)
	run(ctx, client, "exp1: full-prompt + 3-tasks ", "", minimalTasks)

	// Exp 2: full prompt + full-size input (~48 tasks)
	run(ctx, client, "exp2: full-prompt + 48-tasks", "", fullTasks)

	// Exp 3: simple prompt + minimal input
	run(ctx, client, "exp3: simple-prompt + 3-tasks", simplePrompt, minimalTasks)
}
