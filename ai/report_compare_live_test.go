//go:build report_compare

package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"message-consolidator/ai/core"
	"message-consolidator/store"
)

// Why: replays the SAME dumped report payload against multiple models via the raw
// transport (bypasses report_summary.prompt frontmatter model pin + the 180s prod
// timeout) to compare quality and measure real per-model latency.
// Run after TestReportCompare_DumpInput:
//
//	set -a; source .env; set +a; REPORT_COMPARE_OUT=<dir> \
//	  go test -tags=report_compare ./ai/ -run TestReportCompare_Generate -v -timeout 40m
func TestReportCompare_Generate(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping report compare")
	}
	outDir := os.Getenv("REPORT_COMPARE_OUT")
	if outDir == "" {
		t.Fatal("REPORT_COMPARE_OUT not set")
	}
	payload, err := os.ReadFile(filepath.Join(outDir, "input.txt"))
	if err != nil {
		t.Fatalf("read input (run TestReportCompare_DumpInput first): %v", err)
	}
	window, err := os.ReadFile(filepath.Join(outDir, "window.txt"))
	if err != nil {
		t.Fatalf("read window: %v", err)
	}
	email := "jjsong@whatap.io"
	if v := os.Getenv("REPORT_COMPARE_EMAIL"); v != "" {
		email = v
	}

	c, err := NewAIClient(context.Background(), ProviderConfig{
		Provider:        "deepseek",
		DeepSeekAPIKey:  key,
		DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
	})
	if err != nil {
		t.Fatalf("NewAIClient(deepseek): %v", err)
	}

	parsed := core.LoadPrompt(core.PromptReportSummary)
	data := core.ExtractionContext{
		MessagePayload:   string(payload),
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		StaleThreshold:   store.GetStaleThresholdWorkingDays(),
		CurrentUserEmail: email,
		ReportWindow:     string(window),
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		t.Fatalf("render prompt: %v", err)
	}

	models := []string{"deepseek-v4-pro", "deepseek-v4-flash:0731"}
	if v := os.Getenv("REPORT_COMPARE_MODELS"); v != "" {
		models = strings.Split(v, ",")
	}

	for _, model := range models {
		req := LLMRequest{
			Model:       model,
			System:      rendered,
			Temperature: 0.1,
			MaxTokens:   ReportMaxTokens,
			Thinking:    ThinkOn,
		}
		start := time.Now()
		// Why: 600s single attempt — prod's 180s is the suspected failure cause,
		// so the harness must observe the TRUE latency, not reproduce the timeout.
		resp, genErr := c.transport.Generate(context.Background(), req, 600*time.Second, 0)
		elapsed := time.Since(start)
		if genErr != nil {
			t.Errorf("[%s] FAILED after %s: %v", model, elapsed.Round(time.Second), genErr)
			continue
		}
		name := strings.NewReplacer(":", "_", "/", "_").Replace(model) + ".md"
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(resp.Text), 0o644); err != nil {
			t.Fatalf("write output: %v", err)
		}
		t.Logf("[%s] elapsed=%s finish=%s prompt=%d completion=%d reasoning=%d out=%s",
			model, elapsed.Round(time.Second), resp.FinishReason,
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.ReasoningTokens, name)
	}
}
