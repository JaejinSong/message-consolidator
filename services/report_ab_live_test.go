//go:build report_ab

// A/B harness that runs the real report_summary prompt over the SAME real task payload
// through two or more models, so report quality can be compared without a deploy.
// Read-only against the live database; every model's output and a structural scorecard
// are written to REPORT_AB_OUT for side-by-side review.
//
//	REPORT_AB_EMAIL=you@example.com \
//	REPORT_AB_START=2026-08-29 REPORT_AB_END=2026-09-04 \
//	REPORT_AB_MODELS='deepseek-v4-pro:on,glm-5.3-flash:on' \
//	REPORT_AB_OUT=/tmp/report-ab \
//	go test -tags report_ab ./services/ -run TestReportAB -v -timeout 40m
//
// Credentials come from the repo .env (TURSO_DATABASE_URL, TURSO_AUTH_TOKEN,
// DEEPSEEK_API_KEY, DEEPSEEK_BASE_URL) exactly as the other live tests load them.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/ai/core"
	"message-consolidator/store"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

const abDefaultBaseURL = "https://ollama.com/v1"

// abModel is one arm of the comparison: a model id plus its thinking setting.
type abModel struct {
	ID       string
	Thinking string // on | off | default
}

// abResult carries everything the scorecard is computed from.
type abResult struct {
	Model            string
	Thinking         string
	Latency          time.Duration
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
	FinishReason     string
	Text             string
}

func TestReportAB(t *testing.T) {
	loadABEnv(t)

	email := envOrSkip(t, "REPORT_AB_EMAIL")
	start := envOrDefault("REPORT_AB_START", time.Now().AddDate(0, 0, -6).Format("2006-01-02"))
	end := envOrDefault("REPORT_AB_END", time.Now().Format("2006-01-02"))
	outDir := envOrDefault("REPORT_AB_OUT", filepath.Join(os.TempDir(), "report-ab"))
	models := parseABModels(t, envOrDefault("REPORT_AB_MODELS", "deepseek-v4-pro:on,glm-5.3-flash:on"))

	apiKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set - skipping report A/B")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}

	payload, window := buildABPayload(t, email, start, end, outDir)

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = envOrDefault("DEEPSEEK_BASE_URL", abDefaultBaseURL)
	client := openai.NewClientWithConfig(cfg)

	results := make([]abResult, 0, len(models))
	for _, m := range models {
		res := runABModel(t, client, m, email, payload, window)
		if res == nil {
			continue
		}
		writeABFile(t, outDir, m.ID+".md", res.Text)
		results = append(results, *res)
	}
	if len(results) == 0 {
		t.Fatal("no model produced a report")
	}

	report := scoreABResults(payload, results)
	writeABFile(t, outDir, "scorecard.txt", report)
	t.Logf("\n%s\noutputs: %s", report, outDir)
}

// buildABPayload reproduces the production report input and persists it next to the
// model outputs so every arm is provably scored against the same bytes.
func buildABPayload(t *testing.T, email, start, end, outDir string) (payload, window string) {
	t.Helper()
	db, err := openReadOnlyDB()
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	activity, stalled, err := fetchABLogs(ctx, db, email, start, end)
	if err != nil {
		t.Fatalf("fetchABLogs: %v", err)
	}
	if len(activity) == 0 && len(stalled) == 0 {
		t.Fatalf("no tasks for %s in %s ~ %s", email, start, end)
	}

	svc := NewReportsService(nil, nil, nil, ReportConfig{})
	payload, truncated := svc.PrepareLogsForAI(email, activity, stalled)
	t.Logf("input: activity=%d stalled=%d bytes=%d truncated=%v", len(activity), len(stalled), len(payload), truncated)

	writeABFile(t, outDir, "input_payload.txt", payload)
	return payload, start + " ~ " + end
}

// runABModel renders the report prompt exactly as ai.GenerateReportSummary does and
// sends it to one model.
func runABModel(t *testing.T, client *openai.Client, m abModel, email, payload, window string) *abResult {
	t.Helper()
	parsed := core.LoadPrompt(core.PromptReportSummary)
	rendered, err := parsed.Render(core.ExtractionContext{
		MessagePayload:   payload,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		StaleThreshold:   store.GetStaleThresholdWorkingDays(),
		CurrentUserEmail: email,
		ReportWindow:     window,
	})
	if err != nil {
		t.Fatalf("render report prompt: %v", err)
	}

	req := openai.ChatCompletionRequest{
		Model: m.ID,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: rendered},
			// Why: mirrors deepseekTransport - a chat completion needs one non-empty
			// user message even when the whole instruction lives in the system role.
			{Role: openai.ChatMessageRoleUser, Content: "."},
		},
		Temperature:     0.1,
		MaxTokens:       ai.ReportMaxTokens,
		ReasoningEffort: abReasoningEffort(m.Thinking),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	started := time.Now()
	resp, err := client.CreateChatCompletion(ctx, req)
	elapsed := time.Since(started)
	if err != nil {
		t.Errorf("%s: chat completion failed after %s: %v", m.ID, elapsed.Round(time.Millisecond), err)
		return nil
	}
	if len(resp.Choices) == 0 {
		t.Errorf("%s: empty response", m.ID)
		return nil
	}

	choice := resp.Choices[0]
	res := &abResult{
		Model: m.ID, Thinking: m.Thinking, Latency: elapsed,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		FinishReason:     string(choice.FinishReason),
		Text:             choice.Message.Content,
	}
	if d := resp.Usage.CompletionTokensDetails; d != nil {
		res.ReasoningTokens = d.ReasoningTokens
	}
	if d := resp.Usage.PromptTokensDetails; d != nil {
		res.CachedTokens = d.CachedTokens
	}
	return res
}

func abReasoningEffort(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on":
		return "medium"
	case "off":
		return "none"
	default:
		return ""
	}
}

func parseABModels(t *testing.T, spec string) []abModel {
	t.Helper()
	var out []abModel
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, thinking := part, "on"
		// Why: split on the LAST colon so tag-bearing ids like deepseek-v4-flash:0731
		// keep their tag and only the trailing thinking flag is peeled off.
		if i := strings.LastIndex(part, ":"); i > 0 {
			if suffix := part[i+1:]; suffix == "on" || suffix == "off" || suffix == "default" {
				id, thinking = part[:i], suffix
			}
		}
		out = append(out, abModel{ID: id, Thinking: thinking})
	}
	if len(out) == 0 {
		t.Fatal("REPORT_AB_MODELS resolved to no models")
	}
	return out
}

func loadABEnv(t *testing.T) {
	t.Helper()
	for _, p := range []string{".env", "../.env", filepath.Join(os.Getenv("HOME"), "message-consolidator", ".env")} {
		if err := godotenv.Load(p); err == nil {
			t.Logf("loaded env from %s", p)
			return
		}
	}
	t.Log("no .env found - relying on the ambient environment")
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		t.Skipf("%s not set - skipping report A/B", key)
	}
	return v
}

func writeABFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// ---------- scorecard ----------

var (
	abQuoteRE   = regexp.MustCompile(`"([^"\n]{3,})"`)
	abJSONRE    = regexp.MustCompile("(?s)```json\\s*(.*?)```")
	abTaskLine  = regexp.MustCompile(`(?m)^- \[[V ]\]`)
	abHeaders   = []string{"## [Operations & Strategy Overview]", "## [Key Insights]", "## [Activity]", "## [Stalled Tasks]"}
	abBadCustom = []string{"Gmail", "Slack", "DM", "Inbox", "Sent", "Drafts", "Telegram", "WhatsApp"}
)

type abActivityRow struct {
	Customer string `json:"customer"`
	Count    int    `json:"count"`
	Summary  string `json:"summary"`
}

// scoreABResults renders one table per objective check the prompt's own rules define,
// so a reviewer starts from measured contract adherence rather than a blind read.
func scoreABResults(payload string, results []abResult) string {
	activityLines := countActivityTaskLines(payload)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Report A/B scorecard\ninput task lines in [Activity Tasks]: %d\n\n", activityLines)
	for _, r := range results {
		fmt.Fprintf(&sb, "== %s (thinking=%s)\n", r.Model, r.Thinking)
		fmt.Fprintf(&sb, "  latency          : %s\n", r.Latency.Round(time.Millisecond))
		fmt.Fprintf(&sb, "  tokens           : prompt=%d cached=%d completion=%d reasoning=%d\n",
			r.PromptTokens, r.CachedTokens, r.CompletionTokens, r.ReasoningTokens)
		fmt.Fprintf(&sb, "  finish_reason    : %s\n", r.FinishReason)
		fmt.Fprintf(&sb, "  output chars     : %d\n", len(r.Text))
		fmt.Fprintf(&sb, "  BLUF first line  : %s\n", checkBLUF(r.Text))
		fmt.Fprintf(&sb, "  headers in order : %s\n", checkHeaders(r.Text))
		fmt.Fprintf(&sb, "  quote grounding  : %s\n", checkQuoteGrounding(r.Text, payload))
		fmt.Fprintf(&sb, "  activity json    : %s\n", checkActivityJSON(r.Text, activityLines))
		sb.WriteString("\n")
	}
	return sb.String()
}

func countActivityTaskLines(payload string) int {
	body := payload
	if i := strings.Index(body, "[Activity Tasks]"); i >= 0 {
		body = body[i:]
	}
	if i := strings.Index(body, "[Stalled Tasks"); i >= 0 {
		body = body[:i]
	}
	return len(abTaskLine.FindAllString(body, -1))
}

// checkBLUF enforces rule 1: a single BLUF line of at most 25 words before any header.
func checkBLUF(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "BLUF:") {
			return fmt.Sprintf("FAIL - first line is %q", truncateForLog(line, 60))
		}
		words := len(strings.Fields(strings.TrimPrefix(line, "BLUF:")))
		if words > 25 {
			return fmt.Sprintf("FAIL - %d words (limit 25)", words)
		}
		return fmt.Sprintf("PASS - %d words", words)
	}
	return "FAIL - empty output"
}

func checkHeaders(text string) string {
	pos := -1
	var missing, outOfOrder []string
	for _, h := range abHeaders {
		i := strings.Index(text, h)
		if i < 0 {
			missing = append(missing, h)
			continue
		}
		if i < pos {
			outOfOrder = append(outOfOrder, h)
		}
		pos = i
	}
	switch {
	case len(missing) > 0:
		return "FAIL - missing " + strings.Join(missing, ", ")
	case len(outOfOrder) > 0:
		return "FAIL - out of order " + strings.Join(outOfOrder, ", ")
	}
	return "PASS - all 4 present, in order"
}

// checkQuoteGrounding tests the prompt's hardest rule: every quoted span in Key Insights
// must be a contiguous verbatim slice of the Evidence in the input.
func checkQuoteGrounding(text, payload string) string {
	section := sectionBetween(text, "## [Key Insights]", "## [Activity]")
	if section == "" {
		return "SKIP - no Key Insights section"
	}
	matches := abQuoteRE.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		if strings.Contains(section, "No anomalies detected") {
			return "SKIP - no anomalies detected"
		}
		return "WARN - no quoted evidence"
	}
	var ungrounded []string
	for _, m := range matches {
		if !strings.Contains(payload, m[1]) {
			ungrounded = append(ungrounded, truncateForLog(m[1], 50))
		}
	}
	if len(ungrounded) > 0 {
		return fmt.Sprintf("FAIL - %d/%d not verbatim in input: %s",
			len(ungrounded), len(matches), strings.Join(ungrounded, " | "))
	}
	return fmt.Sprintf("PASS - %d/%d quotes verbatim", len(matches), len(matches))
}

// checkActivityJSON enforces rule 4: one parseable json block, counts summing to the
// number of activity task lines, sorted descending, no channel names as customers.
func checkActivityJSON(text string, activityLines int) string {
	section := sectionBetween(text, "## [Activity]", "## [Stalled Tasks]")
	m := abJSONRE.FindStringSubmatch(section)
	if m == nil {
		return "FAIL - no ```json block in Activity"
	}
	var rows []abActivityRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &rows); err != nil {
		return fmt.Sprintf("FAIL - unparseable: %v", err)
	}

	var problems []string
	sum := 0
	for i, r := range rows {
		sum += r.Count
		if i > 0 && r.Count > rows[i-1].Count {
			problems = append(problems, "not sorted by count DESC")
		}
		for _, bad := range abBadCustom {
			if strings.EqualFold(strings.TrimSpace(r.Customer), bad) || strings.HasPrefix(r.Customer, bad+" (") {
				problems = append(problems, fmt.Sprintf("channel name as customer: %q", r.Customer))
			}
		}
		if strings.TrimSpace(r.Summary) == "" {
			problems = append(problems, fmt.Sprintf("empty summary for %q", r.Customer))
		}
	}
	if sum != activityLines {
		problems = append(problems, fmt.Sprintf("count sum %d != %d input lines", sum, activityLines))
	}
	if len(problems) > 0 {
		return fmt.Sprintf("FAIL - %d buckets, %s", len(rows), strings.Join(dedupe(problems), "; "))
	}
	return fmt.Sprintf("PASS - %d buckets, counts sum to %d", len(rows), sum)
}

func sectionBetween(text, from, to string) string {
	i := strings.Index(text, from)
	if i < 0 {
		return ""
	}
	rest := text[i+len(from):]
	if j := strings.Index(rest, to); j >= 0 {
		return rest[:j]
	}
	return rest
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
