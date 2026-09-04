package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"message-consolidator/ai/core"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"

	"github.com/whatap/go-api/trace"
)

// blufPanelModels is the nomination panel. Why: Ollama's OpenAI-compatible API does not
// support `n`, so best-of-N has to be client-side regardless -- and three frontier models from
// three different labs disagree in more informative ways than one model sampled three times,
// because their disagreement is about judgment rather than sampling noise. The judge model
// (bluf_judge.prompt frontmatter) is deliberately a fourth lab and must not appear here; see
// blufPanel, which enforces that at runtime.
var blufPanelModels = []string{"deepseek-v4-pro", "glm-5.3", "minimax-m3"}

// blufPanelTemps spreads sampling across panel slots. It is what produces divergence on the
// Gemini path, where the panel collapses onto the single frontmatter model.
var blufPanelTemps = []float64{0.15, 0.45, 0.75}

const (
	blufCallTimeout = 181 * time.Second
	blufMaxWords    = 25
	blufMaxRetries  = 1
)

// BLUFResult is the decided BLUF line plus the trail that produced it, so the caller can log
// why this item won without re-running the panel.
type BLUFResult struct {
	Line        string
	CandidateID int64
	Rationale   string
	WhyMissed   string
	Consequence string
	Nominations int
	Panel       []string
}

// blufNomination is one panel member's pick. Field names mirror bluf_nominate.prompt's schema.
type blufNomination struct {
	CandidateID int64   `json:"candidate_id"`
	WhyMissed   string  `json:"why_missed"`
	Consequence string  `json:"consequence"`
	Surprise    string  `json:"surprise"`
	BLUF        string  `json:"bluf"`
	Confidence  float64 `json:"confidence"`
	RunnerUpID  int64   `json:"runner_up_id"`

	model string // set by the caller, never by the model
}

// blufVerdict is the judge's arbitration. Field names mirror bluf_judge.prompt's schema.
type blufVerdict struct {
	WinnerIndex int    `json:"winner_index"`
	CandidateID int64  `json:"candidate_id"`
	BLUF        string `json:"bluf"`
	Rationale   string `json:"rationale"`
	Rejected    []struct {
		Index  int    `json:"index"`
		Reason string `json:"reason"`
	} `json:"rejected"`
}

// SelectBLUF runs the dedicated BLUF pipeline over a pre-ranked candidate list: a panel of
// frontier models each nominate the item the reader most likely missed, then a judge arbitrates
// and phrases the final line. Why a separate stage: the report summary model is chosen for
// cheap high-volume rollup work, and its BLUF rule was competing with four other output
// contracts in one prompt -- which is how BLUF selection collapsed onto input order.
//
// Returns an error rather than a fallback line; the caller degrades to the report prompt's own
// BLUF rule, which still sees the deterministic shortlist in its Stats block.
func (g *AIClient) SelectBLUF(ctx context.Context, email, candidates, window string, reportID store.ReportID) (BLUFResult, error) {
	if g == nil || g.transport == nil {
		return BLUFResult{}, fmt.Errorf("AI client is not initialized")
	}
	if strings.TrimSpace(candidates) == "" {
		return BLUFResult{}, fmt.Errorf("no BLUF candidates to select from")
	}

	start := time.Now()
	judge := core.LoadPrompt(core.PromptBLUFJudge)
	judgeModel := g.resolveModel(judge, g.report)
	noms, panel := g.nominateBLUF(ctx, email, candidates, window, judgeModel, reportID)
	defer func() {
		_ = trace.Step(ctx, g.tracePrefix+"-BLUFSelect", "", int(time.Since(start).Milliseconds()), len(noms))
	}()
	if len(noms) == 0 {
		return BLUFResult{}, fmt.Errorf("BLUF panel produced no usable nomination")
	}

	res := BLUFResult{Nominations: len(noms), Panel: panel}
	if len(noms) == 1 {
		// Why: arbitration between one opinion is a wasted call, not a safer answer.
		applyNomination(&res, noms[0])
		res.Rationale = "single nomination, no arbitration needed"
		return finalizeBLUF(res, noms)
	}

	verdict, err := g.judgeBLUF(ctx, email, candidates, window, judge, judgeModel, noms, reportID)
	if err != nil {
		logger.Warnf("[BLUF] judge failed, falling back to the most confident nomination: %v", err)
		applyNomination(&res, mostConfident(noms))
		res.Rationale = "judge unavailable, highest self-reported confidence"
		return finalizeBLUF(res, noms)
	}
	applyVerdict(&res, verdict, noms)
	return finalizeBLUF(res, noms)
}

// nominateBLUF fans the same candidate list out to every panel slot at once and keeps whatever
// comes back parseable. Reports the panel model ids actually used.
func (g *AIClient) nominateBLUF(ctx context.Context, email, candidates, window, judgeModel string, reportID store.ReportID) ([]blufNomination, []string) {
	parsed := core.LoadPrompt(core.PromptBLUFNominate)
	rendered, err := parsed.Render(blufContext(email, candidates, window, ""))
	if err != nil {
		logger.Warnf("[BLUF] nominate prompt render failed: %v", err)
		return nil, nil
	}
	models := g.blufPanel(parsed, judgeModel)
	out := make([]*blufNomination, len(models))

	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(slot int, model string) {
			defer wg.Done()
			defer safego.Recover("bluf-nominate")
			out[slot] = g.runNomination(ctx, email, rendered, model, blufPanelTemps[slot%len(blufPanelTemps)], reportID)
		}(i, model)
	}
	wg.Wait()

	noms := make([]blufNomination, 0, len(models))
	for _, n := range out {
		if n != nil {
			noms = append(noms, *n)
		}
	}
	return noms, models
}

func (g *AIClient) runNomination(ctx context.Context, email, rendered, model string, temp float64, reportID store.ReportID) *blufNomination {
	req := LLMRequest{
		Model:       model,
		System:      rendered,
		Temperature: temp,
		MaxTokens:   ReportMaxTokens,
		JSONMode:    true,
		Thinking:    ThinkHigh,
	}
	resp, err := g.transport.Generate(ctx, req, blufCallTimeout, blufMaxRetries)
	if err != nil {
		logger.Warnf("[BLUF] nomination from %s failed: %v", model, err)
		if uErr := store.AddTokenUsage(email, "BLUFNominate", model, "failed", reportID, 0, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] BLUFNominate failure attribution: %v", uErr)
		}
		return nil
	}
	logTokenUsage(ctx, email, "BLUFNominate", model, "", reportID, resp.Usage)

	var n blufNomination
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &n); err != nil {
		logger.Warnf("[BLUF] nomination from %s was not parseable JSON: %v", model, err)
		return nil
	}
	if strings.TrimSpace(n.BLUF) == "" || n.CandidateID == 0 {
		logger.Warnf("[BLUF] nomination from %s lacked a bluf line or candidate id", model)
		return nil
	}
	n.model = model
	return &n
}

func (g *AIClient) judgeBLUF(ctx context.Context, email, candidates, window string, parsed *core.ParsedPrompt, model string, noms []blufNomination, reportID store.ReportID) (blufVerdict, error) {
	rendered, err := parsed.Render(blufContext(email, candidates, window, renderNominations(noms)))
	if err != nil {
		return blufVerdict{}, fmt.Errorf("judge prompt render failed: %w", err)
	}
	req := LLMRequest{
		Model:       model,
		System:      rendered,
		Temperature: 0,
		MaxTokens:   ReportMaxTokens,
		JSONMode:    true,
		Thinking:    g.resolveThinking(parsed, modelSpec{model, ThinkHigh}),
	}
	resp, err := g.transport.Generate(ctx, req, blufCallTimeout, blufMaxRetries)
	if err != nil {
		if uErr := store.AddTokenUsage(email, "BLUFJudge", model, "failed", reportID, 0, 0, 0, 0); uErr != nil {
			logger.Warnf("[TOKEN-USAGE] BLUFJudge failure attribution: %v", uErr)
		}
		return blufVerdict{}, err
	}
	logTokenUsage(ctx, email, "BLUFJudge", model, "", reportID, resp.Usage)

	var v blufVerdict
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &v); err != nil {
		return blufVerdict{}, fmt.Errorf("judge verdict was not parseable JSON: %w", err)
	}
	if strings.TrimSpace(v.BLUF) == "" {
		return blufVerdict{}, fmt.Errorf("judge verdict carried no bluf line")
	}
	return v, nil
}

// blufPanel returns the model id per panel slot. The DeepSeek/Ollama path runs the real
// multi-model panel; every other provider repeats its own frontmatter model, so divergence
// comes from blufPanelTemps alone.
//
// The judge model is filtered out. Why: a judge that also nominated is grading its own work,
// which is the one property arbitration exists to avoid -- and nothing else would catch the
// overlap, since a nomination and a verdict are told apart only by which call produced them.
func (g *AIClient) blufPanel(parsed *core.ParsedPrompt, judgeModel string) []string {
	if g.provider != providerDeepSeek {
		model := g.resolveModel(parsed, g.report)
		return []string{model, model, model}
	}
	panel := make([]string, 0, len(blufPanelModels))
	for _, m := range blufPanelModels {
		if m == judgeModel {
			logger.Warnf("[BLUF] panel model %s is also the judge; dropping it from the panel", m)
			continue
		}
		panel = append(panel, m)
	}
	return panel
}

func blufContext(email, candidates, window, nominations string) core.ExtractionContext {
	return core.ExtractionContext{
		MessagePayload:   candidates,
		BLUFNominations:  nominations,
		CurrentTime:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:           "English",
		CurrentUserEmail: email,
		ReportWindow:     window,
	}
}

func renderNominations(noms []blufNomination) string {
	var sb strings.Builder
	for i, n := range noms {
		fmt.Fprintf(&sb, "[N%d] from %s\n", i+1, n.model)
		b, err := json.Marshal(n)
		if err != nil {
			continue
		}
		sb.Write(b)
		sb.WriteString("\n")
	}
	return sb.String()
}

func applyNomination(res *BLUFResult, n blufNomination) {
	res.Line = strings.TrimSpace(n.BLUF)
	res.CandidateID = n.CandidateID
	res.WhyMissed = n.WhyMissed
	res.Consequence = n.Consequence
}

// applyVerdict takes the judge's line, and pulls why_missed/consequence from the nomination it
// picked so the log still records the reasoning behind the winner.
func applyVerdict(res *BLUFResult, v blufVerdict, noms []blufNomination) {
	res.Line = strings.TrimSpace(v.BLUF)
	res.CandidateID = v.CandidateID
	res.Rationale = v.Rationale
	if v.WinnerIndex >= 1 && v.WinnerIndex <= len(noms) {
		won := noms[v.WinnerIndex-1]
		res.WhyMissed = won.WhyMissed
		res.Consequence = won.Consequence
		if res.CandidateID == 0 {
			res.CandidateID = won.CandidateID
		}
	}
	for _, r := range v.Rejected {
		logger.Infof("[BLUF] judge rejected nomination %d: %s", r.Index, r.Reason)
	}
}

// finalizeBLUF enforces the one hard format rule the downstream report contract depends on.
// Why: an over-long line is not truncated -- cutting a BLUF mid-clause loses the "by when" and
// reads as a bug; a shorter grounded nomination is the better answer.
func finalizeBLUF(res BLUFResult, noms []blufNomination) (BLUFResult, error) {
	if blufWordCount(res.Line) <= blufMaxWords {
		return res, nil
	}
	logger.Warnf("[BLUF] winning line was %d words (limit %d), looking for a compliant nomination", blufWordCount(res.Line), blufMaxWords)
	for _, n := range noms {
		if blufWordCount(n.BLUF) <= blufMaxWords {
			applyNomination(&res, n)
			res.Rationale = "winning line exceeded the word limit; used a compliant nomination"
			return res, nil
		}
	}
	return res, nil
}

func blufWordCount(line string) int {
	return len(strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "BLUF:")))
}

func mostConfident(noms []blufNomination) blufNomination {
	best := noms[0]
	for _, n := range noms[1:] {
		if n.Confidence > best.Confidence {
			best = n
		}
	}
	return best
}
