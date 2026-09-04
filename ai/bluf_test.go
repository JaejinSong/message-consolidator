package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"message-consolidator/ai/core"
)

// blufFakeTransport answers per-model so a test can script one reply per panel slot plus the
// judge, and records what each call asked for.
type blufFakeTransport struct {
	mu       sync.Mutex
	replies  map[string]string   // model -> raw response text
	sequence map[string][]string // model -> per-call replies; consumed before falling back to replies
	errs     map[string]error
	requests []LLMRequest
	calls    map[string]int
}

func (f *blufFakeTransport) Generate(_ context.Context, req LLMRequest, _ time.Duration, _ int) (LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	n := f.calls[req.Model]
	f.calls[req.Model]++
	if err, ok := f.errs[req.Model]; ok {
		return LLMResponse{}, err
	}
	if seq := f.sequence[req.Model]; n < len(seq) {
		return LLMResponse{Text: seq[n]}, nil
	}
	return LLMResponse{Text: f.replies[req.Model]}, nil
}

func (f *blufFakeTransport) callsFor(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[model]
}

func (f *blufFakeTransport) requestFor(model string) (LLMRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r.Model == model {
			return r, true
		}
	}
	return LLMRequest{}, false
}

func (f *blufFakeTransport) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func nominationJSON(id int64, line string, confidence float64) string {
	n := blufNomination{
		CandidateIDs: []int64{id, id + 1000},
		Pattern:      "ownerless items re-raised after weeks quiet",
		LeadStake:    "renewal path unconfirmed",
		BLUF:         line,
		Confidence:   confidence,
	}
	b, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func verdictJSON(winner int, id int64, line string) string {
	v := blufVerdict{WinnerIndex: winner, CandidateIDs: []int64{id, id + 1000}, BLUF: line, Rationale: "tier (a) committed contract"}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func blufTestClient(f *blufFakeTransport) *AIClient {
	return &AIClient{
		transport:   f,
		provider:    providerDeepSeek,
		tracePrefix: "DeepSeek",
		report:      modelSpec{"kimi-k3", ThinkHigh},
	}
}

const blufTestCandidates = "[BLUF Candidates]\n[C1] score=175 id=12440\n  Task: Identify whitelisting domain\n"

func TestSelectBLUF_JudgeVerdictWins(t *testing.T) {
	f := &blufFakeTransport{replies: map[string]string{
		"deepseek-v4-pro": nominationJSON(12440, "Assign an owner for the Puspakom whitelisting domain -- 67 days quiet.", 0.9),
		"glm-5.3":         nominationJSON(12611, "Andy Phan must confirm the FIF SaaS renewal path.", 0.5),
		"minimax-m3":      nominationJSON(12440, "Assign an owner for the whitelisting domain.", 0.4),
		"kimi-k3":         verdictJSON(2, 12611, "Andy Phan must confirm the FIF SaaS renewal path -- contract expiring, untouched 65 days."),
	}}
	res, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "2026-08-29 ~ 2026-09-04", 0)
	if err != nil {
		t.Fatalf("SelectBLUF: %v", err)
	}
	if len(res.CandidateIDs) == 0 || res.CandidateIDs[0] != 12611 {
		t.Errorf("covers = %v, want the judge's lead 12611", res.CandidateIDs)
	}
	if !strings.HasPrefix(res.Line, "Andy Phan must confirm") {
		t.Errorf("line = %q, want the judge's rewritten line", res.Line)
	}
	// Why: the judge's pick must carry the winning draft's reasoning, not the first one's.
	if res.LeadStake != "renewal path unconfirmed" {
		t.Errorf("lead stake = %q, want it carried from the winning draft", res.LeadStake)
	}
}

func TestSelectBLUF_PanelRunsEveryModelAtHighReasoning(t *testing.T) {
	f := &blufFakeTransport{replies: map[string]string{
		"deepseek-v4-pro": nominationJSON(1, "One must act now.", 0.5),
		"glm-5.3":         nominationJSON(2, "Two must act now.", 0.5),
		"minimax-m3":      nominationJSON(3, "Three must act now.", 0.5),
		"kimi-k3":         verdictJSON(1, 1, "One must act now."),
	}}
	if _, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0); err != nil {
		t.Fatalf("SelectBLUF: %v", err)
	}
	for _, model := range blufPanelModels {
		req, ok := f.requestFor(model)
		if !ok {
			t.Errorf("panel model %s was never called", model)
			continue
		}
		if req.Thinking != ThinkHigh {
			t.Errorf("%s ran at thinking=%v, want ThinkHigh", model, req.Thinking)
		}
		if !req.JSONMode {
			t.Errorf("%s ran without JSON mode; the schema is only enforced by response_format", model)
		}
	}
	// Why: identical temperatures across slots would make the panel's disagreement collapse.
	temps := map[float64]bool{}
	for _, r := range f.requests {
		if r.Temperature > 0 {
			temps[r.Temperature] = true
		}
	}
	if len(temps) < 2 {
		t.Errorf("panel used %d distinct temperatures, want a spread: %v", len(temps), temps)
	}
}

func TestSelectBLUF_SurvivesPartialPanelFailure(t *testing.T) {
	f := &blufFakeTransport{
		replies: map[string]string{
			"glm-5.3": nominationJSON(12611, "Andy Phan must confirm the FIF SaaS renewal path.", 0.7),
			"kimi-k3": verdictJSON(1, 12611, "Andy Phan must confirm the FIF SaaS renewal path."),
		},
		errs: map[string]error{"deepseek-v4-pro": fmt.Errorf("upstream 503"), "minimax-m3": fmt.Errorf("upstream 503")},
	}
	res, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0)
	if err != nil {
		t.Fatalf("SelectBLUF should survive one dead panel member: %v", err)
	}
	if res.Nominations != 1 {
		t.Errorf("nominations = %d, want 1 surviving", res.Nominations)
	}
	// Why: a single opinion needs no arbitration, so the judge call must be skipped entirely.
	if _, called := f.requestFor("kimi-k3"); called {
		t.Error("judge was called to arbitrate a single nomination")
	}
}

func TestSelectBLUF_FallsBackWhenJudgeFails(t *testing.T) {
	f := &blufFakeTransport{
		replies: map[string]string{
			"deepseek-v4-pro": nominationJSON(1, "Low confidence line here.", 0.2),
			"glm-5.3":         nominationJSON(2, "High confidence line here.", 0.95),
			"minimax-m3":      nominationJSON(3, "Middling confidence line here.", 0.6),
		},
		errs: map[string]error{"kimi-k3": fmt.Errorf("judge timeout")},
	}
	res, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0)
	if err != nil {
		t.Fatalf("SelectBLUF should degrade when the judge fails: %v", err)
	}
	if len(res.CandidateIDs) == 0 || res.CandidateIDs[0] != 2 {
		t.Errorf("covers = %v, want the most confident draft (lead 2)", res.CandidateIDs)
	}
}

func TestSelectBLUF_ErrorsWhenWholePanelFails(t *testing.T) {
	f := &blufFakeTransport{errs: map[string]error{
		"deepseek-v4-pro": fmt.Errorf("boom"), "glm-5.3": fmt.Errorf("boom"),
		"minimax-m3": fmt.Errorf("boom"), "kimi-k3": fmt.Errorf("boom"),
	}}
	// Why: the caller degrades to the in-prompt BLUF rule, so this stage must report failure
	// rather than inventing a line of its own.
	if _, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0); err == nil {
		t.Fatal("want an error when every panel member fails")
	}
}

func TestSelectBLUF_RejectsUnparseableAndEmptyNominations(t *testing.T) {
	f := &blufFakeTransport{replies: map[string]string{
		"deepseek-v4-pro": "not json at all",
		"glm-5.3":         `{"candidate_ids": [5], "bluf": "   "}`,
		"minimax-m3":      `{"bluf": "no candidate ids at all"}`,
		"kimi-k3":         verdictJSON(1, 5, "Should never be reached."),
	}}
	if _, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0); err == nil {
		t.Fatal("want an error when no nomination is both parseable and non-empty")
	}
	if n := f.callCount(); n != len(blufPanelModels) {
		t.Errorf("made %d calls, want only the %d panel calls (no judge)", n, len(blufPanelModels))
	}
}

func TestSelectBLUF_JudgeGetsOneRewriteWhenOverLimit(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", blufMaxWords+6))
	f := &blufFakeTransport{
		replies: map[string]string{
			"deepseek-v4-pro": nominationJSON(1, "One draft sentence here.", 0.4),
			"glm-5.3":         nominationJSON(2, "Two draft sentence here.", 0.4),
			"minimax-m3":      nominationJSON(3, "Three draft sentence here.", 0.4),
		},
		sequence: map[string][]string{"kimi-k3": {verdictJSON(1, 1, long), verdictJSON(1, 1, "Rewritten within the limit, pattern then stake.")}},
	}
	res, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0)
	if err != nil {
		t.Fatalf("SelectBLUF: %v", err)
	}
	if n := f.callsFor("kimi-k3"); n != 2 {
		t.Errorf("judge called %d times, want exactly one retry (2)", n)
	}
	if !strings.HasPrefix(res.Line, "Rewritten within the limit") {
		t.Errorf("line = %q, want the judge's rewrite", res.Line)
	}
	// Why: the retry must tell the judge what went wrong, not just re-ask.
	second := f.requests[len(f.requests)-1]
	if !strings.Contains(second.System, "[Judge retry]") || !strings.Contains(second.System, long[:40]) {
		t.Error("retry prompt did not carry the retry hint with the previous sentence")
	}
}

func TestSelectBLUF_OverlongAfterRetryFallsBackAndKeepsVerdictRationale(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", blufMaxWords+6))
	f := &blufFakeTransport{replies: map[string]string{
		"deepseek-v4-pro": nominationJSON(1, long, 0.4),
		"glm-5.3":         nominationJSON(2, "Andy Phan must confirm the FIF SaaS renewal path.", 0.4),
		"minimax-m3":      nominationJSON(3, long, 0.4),
		"kimi-k3":         verdictJSON(1, 1, long), // over the limit on both passes
	}}
	res, err := blufTestClient(f).SelectBLUF(context.Background(), "me@example.com", blufTestCandidates, "w", 0)
	if err != nil {
		t.Fatalf("SelectBLUF: %v", err)
	}
	if blufWordCount(res.Line) > blufMaxWords {
		t.Errorf("line is %d words (limit %d): %q", blufWordCount(res.Line), blufMaxWords, res.Line)
	}
	// Why: truncating mid-clause would drop the lead stake and read as a bug, so the shorter
	// grounded draft must be substituted whole.
	if len(res.CandidateIDs) == 0 || res.CandidateIDs[0] != 2 {
		t.Errorf("covers = %v, want the compliant draft (lead 2)", res.CandidateIDs)
	}
	// Why: the judge still decided the subject; only the wording fell back.
	if !strings.Contains(res.Rationale, "tier (a) committed contract") || !strings.Contains(res.Rationale, "fallback") {
		t.Errorf("rationale lost the judge's verdict or the fallback note: %q", res.Rationale)
	}
	if n := f.callsFor("kimi-k3"); n != 2 {
		t.Errorf("judge called %d times, want 2 (one retry, no more)", n)
	}
}

func TestReasoningEffort_MapsThinkHighToHigh(t *testing.T) {
	cases := map[ThinkingMode]string{
		ThinkHigh:    "high",
		ThinkOn:      "medium",
		ThinkOff:     "none",
		ThinkDefault: "",
	}
	for mode, want := range cases {
		if got := reasoningEffort(mode); got != want {
			t.Errorf("reasoningEffort(%v) = %q, want %q", mode, got, want)
		}
	}
}

func TestParseThinkingMode_AcceptsHighAndMax(t *testing.T) {
	for _, in := range []string{"high", "HIGH", " max ", "Max"} {
		if got := parseThinkingMode(in, ThinkOff); got != ThinkHigh {
			t.Errorf("parseThinkingMode(%q) = %v, want ThinkHigh", in, got)
		}
	}
	if got := parseThinkingMode("nonsense", ThinkOn); got != ThinkOn {
		t.Errorf("unknown value must fall back to the stage default, got %v", got)
	}
}

func TestBLUFPanel_ExcludesTheJudgeModel(t *testing.T) {
	f := &blufFakeTransport{}
	g := blufTestClient(f)
	for _, judge := range append([]string{"kimi-k3"}, blufPanelModels...) {
		panel := g.blufPanel(nil, judge)
		for _, m := range panel {
			if m == judge {
				t.Errorf("judge %q appeared in its own panel %v", judge, panel)
			}
		}
		if len(panel) == 0 {
			t.Errorf("panel emptied out for judge %q", judge)
		}
	}
}

func TestBLUFPrompts_LoadRenderAndDeclareHighReasoning(t *testing.T) {
	g := blufTestClient(&blufFakeTransport{})
	cases := []struct {
		name   core.PromptName
		needle string
	}{
		{core.PromptBLUFNominate, "candidate_ids"},
		{core.PromptBLUFJudge, "winner_index"},
	}
	for _, tc := range cases {
		parsed := core.LoadPrompt(tc.name)
		if parsed.Meta.Name == "" {
			t.Fatalf("%s did not load; frontmatter is missing or malformed", tc.name)
		}
		rendered, err := parsed.Render(blufContext("me@example.com", "[BLUF Candidates]\n[C1] id=1\n", "w", "[N1]\n{}\n"))
		if err != nil {
			t.Fatalf("%s failed to render: %v", tc.name, err)
		}
		if !strings.Contains(rendered, tc.needle) {
			t.Errorf("%s rendered without its output schema (missing %q)", tc.name, tc.needle)
		}
		if !strings.Contains(rendered, "[C1] id=1") {
			t.Errorf("%s dropped the candidate payload", tc.name)
		}
		// Why: these stages exist to reason hard; a frontmatter typo silently downgrades them
		// to the stage default without any runtime signal.
		if got := g.resolveThinking(parsed, modelSpec{"", ThinkOff}); got != ThinkHigh {
			t.Errorf("%s resolved thinking to %v, want ThinkHigh", tc.name, got)
		}
		if g.resolveModel(parsed, modelSpec{"", ThinkOff}) == "" {
			t.Errorf("%s declares no deepseekModel", tc.name)
		}
	}
}

func TestBLUFPrompts_JudgeIsNotAPanelModel(t *testing.T) {
	// Why: the runtime filter in blufPanel is a safety net, not the design -- if the shipped
	// judge is also a panel model the panel silently shrinks from three voices to two.
	g := blufTestClient(&blufFakeTransport{})
	judgeModel := g.resolveModel(core.LoadPrompt(core.PromptBLUFJudge), g.report)
	for _, m := range blufPanelModels {
		if m == judgeModel {
			t.Fatalf("judge %q is also a panel model; the panel needs a model from a fourth lab", judgeModel)
		}
	}
	if !strings.Contains(rendersJudgeNominations(t), "[N1]") {
		t.Error("judge prompt does not inject the nominations block")
	}
}

func rendersJudgeNominations(t *testing.T) string {
	t.Helper()
	out, err := core.LoadPrompt(core.PromptBLUFJudge).Render(blufContext("me@example.com", "c", "w", "[N1]\n{}\n"))
	if err != nil {
		t.Fatalf("judge render: %v", err)
	}
	return out
}
