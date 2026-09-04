//go:build deepseek_live

// Live smoke tests for the BLUF stage against Ollama Cloud. Excluded from the default build
// by the deepseek_live tag and skipped without DEEPSEEK_API_KEY:
//
//	set -a; source .env; set +a; go test -tags=deepseek_live ./ai/ -run TestLive_BLUF -v
package ai

import (
	"context"
	"testing"
	"time"

	"message-consolidator/ai/core"
)

// TestLive_BLUF_PanelModelsExist fails fast on a retired or mistyped model tag. Why: a 404 on
// one panel member is otherwise swallowed as "nomination failed" and the panel silently shrinks.
func TestLive_BLUF_PanelModelsExist(t *testing.T) {
	tr := liveTransport(t)
	judge := core.LoadPrompt(core.PromptBLUFJudge).Meta.DeepSeekModel
	for _, model := range append(append([]string{}, blufPanelModels...), judge) {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			start := time.Now()
			resp, err := tr.Generate(ctx, LLMRequest{
				Model: model, System: "Reply with exactly: pong", User: "ping",
				Temperature: 0, MaxTokens: 512, Thinking: ThinkHigh,
			}, 120*time.Second, 0)
			if err != nil {
				t.Fatalf("%s unreachable: %v", model, err)
			}
			t.Logf("%s ok in %s: %q (prompt=%d completion=%d reasoning=%d)", model, time.Since(start).Round(time.Millisecond),
				resp.Text, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.ReasoningTokens)
		})
	}
}

const liveBLUFCandidates = `[BLUF Candidates]
[C1] score=175 id=12440
  Task: Identify link and domain for whitelisting AI analysis recommendation
  Where: whatsapp / Internal Puspakom WhaTap IFC | Topic: Puspakom
  Who: Diana (Customer) -> shared (External)
  Signals: thread carries 8 messages; open 56 working days; resurfaced after 67 calendar days quiet; no named owner; risk wording in evidence
  Evidence: "Raising this again - the whitelisting domain is still blocking our analysis."
[C2] score=162 id=12697
  Task: Progress SAP Ariba registration with DPM partner
  Where: slack / biz-global-thailand | Topic: Ariba
  Who: Golf (Internal) -> shared (External)
  Signals: thread carries 5 messages; open 41 working days; resurfaced after 38 calendar days quiet; no named owner; risk wording in evidence
  Evidence: "Ariba registration is still stuck waiting on the partner."
[C3] score=140 id=12611
  Task: Follow up on contract expiration for FIF SaaS APM Server DB Browser
  Where: gmail / Gmail | Topic: Fif
  Who: billing@fif.co.id (External) -> Andy Phan (Internal)
  Signals: thread carries 7 messages; open 47 working days; never touched since created 65 calendar days ago; external requester waiting
  Evidence: "The FIF SaaS contract expires soon and nobody has confirmed the renewal path."
[C4] score=58 id=13173
  Task: Investigate MySQL error on Notihub account
  Where: whatsapp / Adira - Whatap Tech | Topic: Adira
  Who: Sermon (Customer) -> shared (External)
  Signals: topic mentioned 2x; open 2 working days; no named owner; risk wording in evidence
  Evidence: "The Notihub account shows a MySQL error we cannot explain."
[C5] score=26 id=13190
  Task: Prepare POC review report for Puspakom next review
  Where: whatsapp / Internal Puspakom WhaTap IFC | Topic: Puspakom
  Who: Diana (Customer) -> Andy Phan (Internal)
  Signals: topic mentioned 2x
  Evidence: "Please prepare the review report before the next Puspakom review."
`

// TestLive_BLUF_EndToEnd runs the real panel + judge over the fixture that reproduces the
// observed defect and prints the decision trail for eyeballing.
func TestLive_BLUF_EndToEnd(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := c.SelectBLUF(ctx, "live@example.com", liveBLUFCandidates, "2026-08-29 ~ 2026-09-04", 0)
	if err != nil {
		t.Fatalf("SelectBLUF: %v", err)
	}
	t.Logf("elapsed=%s panel=%v nominations=%d", time.Since(start).Round(time.Second), res.Panel, res.Nominations)
	t.Logf("BLUF (%d words): %s", blufWordCount(res.Line), res.Line)
	t.Logf("candidate=%d rationale=%q", res.CandidateID, res.Rationale)
	t.Logf("why_missed=%q consequence=%q", res.WhyMissed, res.Consequence)
	if res.CandidateID == 13190 {
		t.Errorf("stage picked the newest single-mention task (C5) -- the exact defect this stage exists to remove")
	}
	if blufWordCount(res.Line) > blufMaxWords {
		t.Errorf("line exceeds %d words", blufMaxWords)
	}
}
