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
// support `n`, so best-of-N has to be client-side regardless -- and models from three
// different labs disagree in more informative ways than one model sampled three times,
// because their disagreement is about judgment rather than sampling noise. The judge model
// (bluf_judge.prompt frontmatter) is deliberately a fourth lab and must not appear here; see
// blufPanel, which enforces that at runtime.
//
// The DeepSeek slot is the flash tier, not the frontier tier its siblings sit at: v4-pro is
// retired 2026-09-14 and no replacement was picked on evidence, because nothing here is
// measured yet -- the panel has run 4 times total and its output is logged, never stored.
// Treat this slot as provisional until the offline replay harness can compare candidates on
// self-consistency and panel agreement.
var blufPanelModels = []string{"deepseek-v4.1-flash", "glm-5.3", "minimax-m3"}

// blufPanelTemps spreads sampling across panel slots. It is what produces divergence on the
// Gemini path, where the panel collapses onto the single frontmatter model.
var blufPanelTemps = []float64{0.15, 0.45, 0.75}

const (
	blufCallTimeout = 181 * time.Second
	blufMaxWords    = 40
	blufMaxRetries  = 1
)

// BLUFResult is the decided BLUF sentence plus the trail that produced it, so the caller can
// log what it covers and why without re-running the panel.
type BLUFResult struct {
	Line         string
	CandidateIDs []int64
	Rationale    string
	Pattern      string
	LeadStake    string
	Nominations  int
	Panel        []string
}

// blufNomination is one panel member's draft. Field names mirror bluf_nominate.prompt's schema.
type blufNomination struct {
	CandidateIDs []int64 `json:"candidate_ids"`
	Pattern      string  `json:"pattern"`
	LeadStake    string  `json:"lead_stake"`
	BLUF         string  `json:"bluf"`
	Confidence   float64 `json:"confidence"`

	model string // set by the caller, never by the model
}

// blufVerdict is the judge's arbitration. Field names mirror bluf_judge.prompt's schema.
type blufVerdict struct {
	WinnerIndex  int     `json:"winner_index"`
	CandidateIDs []int64 `json:"candidate_ids"`
	BLUF         string  `json:"bluf"`
	Rationale    string  `json:"rationale"`
	Rejected     []struct {
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

	verdict, err := g.judgeBLUF(ctx, email, candidates, window, judge, judgeModel, noms, "", reportID)
	if err != nil {
		logger.Warnf("[BLUF] judge failed, falling back to the most confident nomination: %v", err)
		applyNomination(&res, mostConfident(noms))
		res.Rationale = "judge unavailable, highest self-reported confidence"
		return finalizeBLUF(res, noms)
	}
	// Why: a synthesis of four or five items overruns the limit more often than a single-item
	// line did, and the judge is the only party that has already weighed all the drafts. One
	// targeted rewrite keeps its verdict; falling straight back to a draft would discard it.
	if words := blufWordCount(verdict.BLUF); words > blufMaxWords {
		hint := fmt.Sprintf("[Judge retry] Your previous final sentence was %d words; the limit is %d. Rewrite it within the limit -- keep the pattern and the lead stake, cut examples first. Previous: %s",
			words, blufMaxWords, verdict.BLUF)
		if retried, rerr := g.judgeBLUF(ctx, email, candidates, window, judge, judgeModel, noms, hint, reportID); rerr == nil {
			verdict = retried
		} else {
			logger.Warnf("[BLUF] judge retry failed, keeping the over-long verdict for fallback: %v", rerr)
		}
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
	if strings.TrimSpace(n.BLUF) == "" || len(n.CandidateIDs) == 0 {
		logger.Warnf("[BLUF] draft from %s lacked a bluf sentence or candidate ids", model)
		return nil
	}
	n.model = model
	return &n
}

// judgeBLUF arbitrates the drafts. retryHint, when non-empty, is appended to the drafts block so
// a second pass can be told exactly what was wrong with its first sentence.
func (g *AIClient) judgeBLUF(ctx context.Context, email, candidates, window string, parsed *core.ParsedPrompt, model string, noms []blufNomination, retryHint string, reportID store.ReportID) (blufVerdict, error) {
	drafts := renderNominations(noms)
	if retryHint != "" {
		drafts += "\n" + retryHint + "\n"
	}
	rendered, err := parsed.Render(blufContext(email, candidates, window, drafts))
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
	res.CandidateIDs = n.CandidateIDs
	res.Pattern = n.Pattern
	res.LeadStake = n.LeadStake
}

// applyVerdict takes the judge's sentence, and pulls pattern/lead stake from the draft it picked
// so the log still records the reasoning behind the winner.
func applyVerdict(res *BLUFResult, v blufVerdict, noms []blufNomination) {
	res.Line = strings.TrimSpace(v.BLUF)
	res.CandidateIDs = v.CandidateIDs
	res.Rationale = v.Rationale
	if v.WinnerIndex >= 1 && v.WinnerIndex <= len(noms) {
		won := noms[v.WinnerIndex-1]
		res.Pattern = won.Pattern
		res.LeadStake = won.LeadStake
		if len(res.CandidateIDs) == 0 {
			res.CandidateIDs = won.CandidateIDs
		}
	}
	for _, r := range v.Rejected {
		logger.Infof("[BLUF] judge rejected nomination %d: %s", r.Index, r.Reason)
	}
}

// finalizeBLUF enforces the one hard format rule the downstream report contract depends on.
// Why: an over-long sentence is not truncated -- cutting it mid-clause drops the lead stake and
// reads as a bug; a shorter grounded draft is the better answer.
func finalizeBLUF(res BLUFResult, noms []blufNomination) (BLUFResult, error) {
	if blufWordCount(res.Line) <= blufMaxWords {
		return res, nil
	}
	over := blufWordCount(res.Line)
	logger.Warnf("[BLUF] winning line was %d words (limit %d), looking for a compliant draft", over, blufMaxWords)
	for _, n := range noms {
		if blufWordCount(n.BLUF) <= blufMaxWords {
			verdict := res.Rationale
			applyNomination(&res, n)
			// Why: the judge's reasoning is still the record of why this subject won; the
			// fallback only explains why the wording is a draft's rather than the judge's.
			res.Rationale = fmt.Sprintf("%s | fallback: judge's %d-word line exceeded %d, used %s's compliant draft", verdict, over, blufMaxWords, n.model)
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
