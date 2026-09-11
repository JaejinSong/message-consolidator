//go:build report_ab

// Offline replay harness for the BLUF stage. Freezes one real candidate set and replays the
// nomination panel over it N times per model, so panel composition can be compared on evidence
// instead of taste. Read-only against the live database.
//
// Why this exists: the production panel has run 4 times in its life and its decisions are
// logged, never stored (see reports_service.go withDecidedBLUF), so nothing about panel
// composition is measurable from history. The premise the panel is built on -- that models from
// different labs disagree more informatively than one model sampled repeatedly -- has never
// been tested. The headline number here tests exactly that: inter-model divergence against
// intra-model divergence on the same frozen input.
//
//	BLUF_REPLAY_EMAIL=you@example.com \
//	BLUF_REPLAY_START=2026-09-04 BLUF_REPLAY_END=2026-09-10 \
//	BLUF_REPLAY_MODELS='deepseek-v4.1-flash:high,glm-5.3:high,minimax-m3:high' \
//	BLUF_REPLAY_REPEATS=3 \
//	BLUF_REPLAY_OUT=/tmp/bluf-replay \
//	go test -tags report_ab ./services/ -run TestBLUFReplay -v -timeout 40m
//
// BLUF_REPLAY_FIXTURE pins the candidate set to a file: written on the first run, reused
// afterwards. Without it every run rebuilds from the database and the arms are no longer
// comparable across days.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"message-consolidator/ai/core"

	openai "github.com/sashabaranov/go-openai"
)

// blufNominationJSON mirrors bluf_nominate.prompt's output schema. Kept local rather than
// imported: ai.blufNomination is unexported, and the harness is deliberately a separate reader
// of the same contract so a silent schema change shows up as a parse failure here.
type blufNominationJSON struct {
	CandidateIDs []int64 `json:"candidate_ids"`
	Pattern      string  `json:"pattern"`
	LeadStake    string  `json:"lead_stake"`
	BLUF         string  `json:"bluf"`
	Confidence   float64 `json:"confidence"`
}

// blufDraft is one nomination call: which arm produced it, on which repeat, and what it cost.
type blufDraft struct {
	Model      string
	Repeat     int
	Nom        blufNominationJSON
	Latency    time.Duration
	Completion int
	Finish     string
	Err        string
}

func (d blufDraft) ok() bool { return d.Err == "" && len(d.Nom.CandidateIDs) > 0 }

// lead is the candidate the draft puts first -- the item it claims the reader most likely
// missed. Agreement is measured on this, not on the whole set, because the BLUF line is about
// one thing.
func (d blufDraft) lead() int64 {
	if len(d.Nom.CandidateIDs) == 0 {
		return 0
	}
	return d.Nom.CandidateIDs[0]
}

func TestBLUFReplay(t *testing.T) {
	loadABEnv(t)

	email := envOrSkip(t, "BLUF_REPLAY_EMAIL")
	start := envOrDefault("BLUF_REPLAY_START", time.Now().AddDate(0, 0, -6).Format("2006-01-02"))
	end := envOrDefault("BLUF_REPLAY_END", time.Now().Format("2006-01-02"))
	outDir := envOrDefault("BLUF_REPLAY_OUT", filepath.Join(os.TempDir(), "bluf-replay"))
	arms := parseABModels(t, envOrDefault("BLUF_REPLAY_MODELS", "deepseek-v4.1-flash:high,glm-5.3:high,minimax-m3:high"))
	repeats := blufReplayRepeats(t)

	apiKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set - skipping BLUF replay")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}

	candidates, window := blufReplayCandidates(t, email, start, end, outDir)

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = envOrDefault("DEEPSEEK_BASE_URL", abDefaultBaseURL)
	client := openai.NewClientWithConfig(cfg)

	rendered := blufRenderPrompt(t, core.PromptBLUFNominate, email, candidates, window, "")
	writeABFile(t, outDir, "nominate_prompt.txt", rendered)

	drafts := blufRunPanel(t, client, arms, rendered, repeats)
	if len(drafts) == 0 {
		t.Fatal("no arm produced a usable nomination")
	}

	report := scoreBLUFReplay(arms, drafts, repeats)
	writeABFile(t, outDir, "scorecard.txt", report)
	writeABFile(t, outDir, "drafts.json", blufDraftsJSON(t, drafts))
	t.Logf("\n%s\noutputs: %s", report, outDir)
}

func blufReplayRepeats(t *testing.T) int {
	t.Helper()
	n, err := strconv.Atoi(envOrDefault("BLUF_REPLAY_REPEATS", "3"))
	if err != nil || n < 1 {
		t.Fatalf("BLUF_REPLAY_REPEATS must be a positive integer, got %q", os.Getenv("BLUF_REPLAY_REPEATS"))
	}
	return n
}

// blufReplayCandidates returns the rendered dossier block every arm is scored against. A
// fixture path freezes it; without one the set is rebuilt from the database each run and
// results stop being comparable across days.
func blufReplayCandidates(t *testing.T, email, start, end, outDir string) (candidates, window string) {
	t.Helper()
	window = start + " ~ " + end
	fixture := strings.TrimSpace(os.Getenv("BLUF_REPLAY_FIXTURE"))
	if fixture != "" {
		if b, err := os.ReadFile(fixture); err == nil && len(b) > 0 {
			t.Logf("candidates: replayed from fixture %s (%d bytes)", fixture, len(b))
			return string(b), window
		}
	}

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
	dossiers := buildBLUFDossiers(activity, stalled, email, time.Now())
	candidates = renderBLUFDossiers(dossiers, email)
	if strings.TrimSpace(candidates) == "" {
		t.Fatalf("no BLUF candidates for %s in %s", email, window)
	}
	t.Logf("candidates: built from DB, activity=%d stalled=%d dossiers=%d", len(activity), len(stalled), len(dossiers))

	writeABFile(t, outDir, "candidates.txt", candidates)
	if fixture != "" {
		if err := os.WriteFile(fixture, []byte(candidates), 0o600); err != nil {
			t.Fatalf("freeze fixture %s: %v", fixture, err)
		}
		t.Logf("candidates: frozen to %s -- later runs replay this exact set", fixture)
	}
	return candidates, window
}

// blufRenderPrompt reproduces ai.blufContext. Why duplicated: blufContext is unexported, and
// the harness has to build the same ExtractionContext to send the same bytes production would.
// A field added there and missed here shows up as an empty section in nominate_prompt.txt.
func blufRenderPrompt(t *testing.T, name core.PromptName, email, candidates, window, nominations string) string {
	t.Helper()
	parsed := core.LoadPrompt(name)
	rendered, err := parsed.Render(core.ExtractionContext{
		MessagePayload:   candidates,
		BLUFNominations:  nominations,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		CurrentUserEmail: email,
		ReportWindow:     window,
	})
	if err != nil {
		t.Fatalf("render %v: %v", name, err)
	}
	return rendered
}

// blufRunPanel fans out across arms the way production does, and repeats each arm in sequence
// so one model's own variance is measured under the same conditions as the panel's.
func blufRunPanel(t *testing.T, client *openai.Client, arms []abModel, rendered string, repeats int) []blufDraft {
	t.Helper()
	perArm := make([][]blufDraft, len(arms))
	var wg sync.WaitGroup
	for i, arm := range arms {
		wg.Add(1)
		go func(slot int, m abModel) {
			defer wg.Done()
			for r := 1; r <= repeats; r++ {
				perArm[slot] = append(perArm[slot], blufRunOne(client, m, rendered, r))
			}
		}(i, arm)
	}
	wg.Wait()

	var out []blufDraft
	for _, arm := range perArm {
		out = append(out, arm...)
	}
	for _, d := range out {
		if d.Err != "" {
			t.Logf("[%s r%d] %s", d.Model, d.Repeat, d.Err)
		}
	}
	return out
}

func blufRunOne(client *openai.Client, m abModel, rendered string, repeat int) blufDraft {
	d := blufDraft{Model: m.ID, Repeat: repeat}
	req := openai.ChatCompletionRequest{
		Model: m.ID,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: rendered},
			{Role: openai.ChatMessageRoleUser, Content: "."},
		},
		// Why: production spreads temperature across panel slots, which confounds lab identity
		// with sampling noise. Held fixed here so divergence is attributable to the model.
		Temperature:     0.15,
		MaxTokens:       65536,
		ReasoningEffort: abReasoningEffort(m.Thinking),
		ResponseFormat:  &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 181*time.Second)
	defer cancel()

	startedAt := time.Now()
	resp, err := client.CreateChatCompletion(ctx, req)
	d.Latency = time.Since(startedAt)
	if err != nil {
		d.Err = "call failed: " + err.Error()
		return d
	}
	if len(resp.Choices) == 0 {
		d.Err = "empty response"
		return d
	}
	d.Completion = resp.Usage.CompletionTokens
	d.Finish = string(resp.Choices[0].FinishReason)
	// Why: SanitizeJSON, not json.Unmarshal -- some panel models fence their JSON in ```json,
	// which production strips the same way. Skipping it reports a model defect that is not one.
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Choices[0].Message.Content)), &d.Nom); err != nil {
		d.Err = "unparseable JSON: " + err.Error()
	}
	return d
}

func blufDraftsJSON(t *testing.T, drafts []blufDraft) string {
	t.Helper()
	b, err := json.MarshalIndent(drafts, "", "  ")
	if err != nil {
		t.Fatalf("marshal drafts: %v", err)
	}
	return string(b)
}

// ---------- scorecard ----------

// scoreBLUFReplay answers the question the panel's design rests on: does swapping labs buy more
// disagreement than re-sampling one model? If inter-model agreement is not meaningfully lower
// than the average within-model agreement, the multi-lab panel is paying three model bills for
// what temperature alone would produce.
func scoreBLUFReplay(arms []abModel, drafts []blufDraft, repeats int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "BLUF replay scorecard (%d arms x %d repeats)\n\n", len(arms), repeats)

	modal := map[string]int64{}
	for _, arm := range arms {
		own := draftsFor(drafts, arm.ID)
		lead, stability := modalLead(own)
		modal[arm.ID] = lead
		writeArmSection(&sb, arm, own, lead, stability)
	}

	leads := make([]int64, 0, len(modal))
	for _, arm := range arms {
		if l := modal[arm.ID]; l != 0 {
			leads = append(leads, l)
		}
	}
	_, interAgree := modeOf(leads)
	intraAgree := meanStability(arms, drafts)

	fmt.Fprintf(&sb, "== panel\n")
	fmt.Fprintf(&sb, "  inter-model agreement on lead : %.2f  (%d arms voting)\n", interAgree, len(leads))
	fmt.Fprintf(&sb, "  intra-model agreement (mean)  : %.2f  (same model, %d repeats)\n", intraAgree, repeats)
	fmt.Fprintf(&sb, "  verdict                       : %s\n", panelVerdict(interAgree, intraAgree, len(leads)))
	return sb.String()
}

func writeArmSection(sb *strings.Builder, arm abModel, own []blufDraft, lead int64, stability float64) {
	fmt.Fprintf(sb, "== %s (thinking=%s)\n", arm.ID, arm.Thinking)
	fmt.Fprintf(sb, "  usable drafts    : %d/%d\n", countOK(own), len(own))
	fmt.Fprintf(sb, "  modal lead id    : %d\n", lead)
	fmt.Fprintf(sb, "  self-consistency : %.2f\n", stability)
	fmt.Fprintf(sb, "  latency / tokens : %s / %d avg\n", avgLatency(own).Round(time.Millisecond), avgCompletion(own))
	fmt.Fprintf(sb, "  over %d words    : %d\n", blufReplayMaxWords, countOverLimit(own))
	for _, d := range own {
		if d.ok() {
			fmt.Fprintf(sb, "    r%d lead=%d conf=%.2f %q\n", d.Repeat, d.lead(), d.Nom.Confidence, truncateForLog(d.Nom.BLUF, 70))
			continue
		}
		fmt.Fprintf(sb, "    r%d FAILED %s\n", d.Repeat, truncateForLog(d.Err, 70))
	}
	sb.WriteString("\n")
}

// blufReplayMaxWords mirrors ai.blufMaxWords, the one hard format rule the report contract
// depends on. A draft over it is not a usable answer even when its judgment is right.
const blufReplayMaxWords = 40

func panelVerdict(inter, intra float64, voting int) string {
	if voting < 2 {
		return "inconclusive - fewer than 2 arms produced a usable lead"
	}
	if inter < intra {
		return "labs disagree more than sampling does - the multi-lab panel is earning its cost"
	}
	if inter == intra {
		return "labs and sampling diverge equally - panel composition is not the active variable"
	}
	return "labs agree MORE than one model does with itself - the panel adds cost, not divergence"
}

func draftsFor(drafts []blufDraft, model string) []blufDraft {
	var out []blufDraft
	for _, d := range drafts {
		if d.Model == model {
			out = append(out, d)
		}
	}
	return out
}

// modalLead returns the most frequently nominated lead and the share of usable drafts that
// agreed on it. A model that picks a different lead every repeat is noise whatever its lab.
func modalLead(drafts []blufDraft) (int64, float64) {
	leads := make([]int64, 0, len(drafts))
	for _, d := range drafts {
		if d.ok() {
			leads = append(leads, d.lead())
		}
	}
	return modeOf(leads)
}

func modeOf(values []int64) (int64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	counts := map[int64]int{}
	for _, v := range values {
		counts[v]++
	}
	keys := make([]int64, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	// Why: map iteration is random, so ties would make the reported mode vary between runs of
	// the same data -- exactly the instability this harness exists to measure.
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	best := keys[0]
	return best, float64(counts[best]) / float64(len(values))
}

func meanStability(arms []abModel, drafts []blufDraft) float64 {
	var sum float64
	var n int
	for _, arm := range arms {
		if _, s := modalLead(draftsFor(drafts, arm.ID)); s > 0 {
			sum += s
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func countOK(drafts []blufDraft) int {
	var n int
	for _, d := range drafts {
		if d.ok() {
			n++
		}
	}
	return n
}

func countOverLimit(drafts []blufDraft) int {
	var n int
	for _, d := range drafts {
		if d.ok() && len(strings.Fields(d.Nom.BLUF)) > blufReplayMaxWords {
			n++
		}
	}
	return n
}

func avgLatency(drafts []blufDraft) time.Duration {
	if len(drafts) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range drafts {
		total += d.Latency
	}
	return total / time.Duration(len(drafts))
}

func avgCompletion(drafts []blufDraft) int {
	if len(drafts) == 0 {
		return 0
	}
	var total int
	for _, d := range drafts {
		total += d.Completion
	}
	return total / len(drafts)
}
