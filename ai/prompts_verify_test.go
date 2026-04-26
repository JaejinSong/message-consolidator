package ai

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// TestPromptsNormalization은 모든 .prompt 파일의 규격을 검증합니다.
func TestPromptsNormalization(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("prompts/*.prompt")
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		f := f // Closure capture
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			verifyPromptFile(t, f)
		})
	}
}

// verifyPromptFile은 개별 프롬프트 파일의 무결성을 검증합니다.
func verifyPromptFile(t *testing.T, path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// 1. Frontmatter 파싱 검증
	parsed, err := ParsePrompt(string(content))
	if err != nil {
		t.Fatalf("frontmatter parse error: %v", err)
	}

	// 2. 템플릿 문법 검증
	tmpl, err := template.New("test").Parse(parsed.Body)
	if err != nil {
		t.Fatalf("template syntax error: %v", err)
	}

	// 3. 필수 변수 사용 여부 및 더미 데이터 실행 검증
	verifyTemplateExecution(t, tmpl)
}

// verifyTemplateExecution은 더미 데이터를 주입하여 템플릿 실행 가능 여부를 확인합니다.
func verifyTemplateExecution(t *testing.T, tmpl *template.Template) {
	dummy := ExtractionContext{
		MessagePayload: "dummy input",
		CurrentTime:    "2026-04-03 12:00:00",
		Version:        "1.0.0",
		Locale:         "ko-KR",
		FewShots:       make([]FewShot, 0),
	}

	if err := tmpl.Execute(io.Discard, dummy); err != nil {
		t.Errorf("template execution failed: %v", err)
	}
}

// TestChatSystemPurposeRule guards the v1.5.0 purpose-first title rule.
// Why: Prevents accidental regression to action-led titles (e.g. "Check and confirm
// availability ..." dropping the PoC scope-alignment purpose).
func TestChatSystemPurposeRule(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/chat_system.prompt")
	if err != nil {
		t.Fatalf("read chat_system: %v", err)
	}
	body := string(content)
	required := []string{
		"Lead with the **WHY (outcome/purpose)**",
		"Layered TASK + PROMISE",
		"preserve the existing task's purpose-phrase",
		"Align POC scope via online session",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("chat_system.prompt missing required rule phrase: %q", token)
		}
	}
}

// TestReportSummaryEvidenceGating guards the v2.4.0 evidence-gating + speaker-stance rules.
// Why: Prevents regression to the v2.3.0 "must flag a risk" mandate that caused
// hallucination of bottlenecks from task titles without Evidence support (Bun.js/XIMPLY case, 2026-04-23).
func TestReportSummaryEvidenceGating(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("read report_summary: %v", err)
	}
	body := string(content)
	required := []string{
		"MUST include verbatim quote",
		"No anomalies detected.",
		"Preserve speaker stance",
		"nominalization",
		"verbatim quote",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("report_summary.prompt missing required rule phrase: %q", token)
		}
	}
	forbidden := []string{
		"At least 1 bullet must flag a risk",
	}
	for _, token := range forbidden {
		if strings.Contains(body, token) {
			t.Errorf("report_summary.prompt must not contain deprecated phrase: %q", token)
		}
	}
}

// TestReportSummaryNoConsequentClause guards the v2.5.0 no-free-form-consequent-clause rule.
// Why: Prevents regression to the v2.4.0 gap where speaker verbs were preserved but a
// trailing unsupported consequent clause was appended (msg 11705, biz-global-tech,
// 2026-04-24 — `... "Verifying every case on my end isn't scalable" ..., which blocks dev requests.`).
func TestReportSummaryNoConsequentClause(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("read report_summary: %v", err)
	}
	body := string(content)
	required := []string{
		"No free-form consequent clauses",
		"verbatim in Evidence",
		", which blocks X",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("report_summary.prompt missing v2.5.0 rule phrase: %q", token)
		}
	}
}

// TestReportSummaryFindingLabel guards the v2.5.1 positive-guide rule.
// Why: v2.5.0 over-corrected by allowing only `"<speaker> states "<quote>" when
// discussing X"` framings, losing the "so what". v2.5.1 re-enables finding labels
// drawn from Evidence vocabulary while keeping the consequent-clause ban.
func TestReportSummaryFindingLabel(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("read report_summary: %v", err)
	}
	body := string(content)
	required := []string{
		"DO add a finding label drawn from Evidence",
		"reuses Evidence vocabulary",
		"TOO WEAK",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("report_summary.prompt missing v2.5.1 rule phrase: %q", token)
		}
	}
}

// TestReportSummaryQuoteContiguity guards the v2.5.3 quote-contiguity + speaker-anchor rule.
// Why: v2.5.2 emitted `Jaejin Song reports verifying every case "isn't scalable" on his end.`
// The LLM sliced the verbatim sentence "Verifying every case on my end isn't scalable" (8
// words, well within ≤10) into a 2-word predicate fragment `"isn't scalable"` and rebuilt
// the subject as paraphrased bullet prose, flipping first-person `my end` to third-person
// `his end`. v2.5.3 mandates contiguous-span quoting and forbids paraphrase around the quote.
func TestReportSummaryQuoteContiguity(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("read report_summary: %v", err)
	}
	body := string(content)
	required := []string{
		"contiguous Evidence span",
		"quote it whole",
		"MUST stay inside the quote",
		"flips first-person stance to third-person attribution",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("report_summary.prompt missing v2.5.3 rule phrase: %q", token)
		}
	}
}

// TestReportSummaryBulletTypes guards the v2.5.2 bullet-type decoupling rule.
// Why: v2.5.1 collapsed Key Insights to 1 bullet because the verbatim-quote mandate
// at L19 conflicted with the structural >40% / cross-source rules at L29-30. v2.5.2
// splits Key Insights into Type A/B/C with independent grounding contracts — Type B
// (concentration) and Type C (cross-source) are unconditional when their thresholds
// are crossed and do NOT require Evidence quotes.
func TestReportSummaryBulletTypes(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("read report_summary: %v", err)
	}
	body := string(content)
	required := []string{
		"Type A — Evidence-backed Risk/Anomaly",
		"Type B — Ownership Concentration",
		"Type C — Cross-source Pattern",
		"single point of failure risk",
		"unconditional",
		"do NOT compete for a single quota",
		"If ALL three types yield zero",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("report_summary.prompt missing v2.5.2 rule phrase: %q", token)
		}
	}
}

// TestNewExtractionNeutralUmbrella guards the neutral-umbrella + paragraph-split rule
// preserved across v1.2.0 → v2.0.0.
// Why: Prevents regression to v1.1.0 where LLM joined independent paragraphs with causal
// connectors (`while`, `because`, `once`) producing compound Tasks that seeded downstream
// consequent-clause hallucinations in report summaries (msg 11705, 2026-04-24).
func TestNewExtractionNeutralUmbrella(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/new_extraction.prompt")
	if err != nil {
		t.Fatalf("read new_extraction: %v", err)
	}
	body := string(content)
	required := []string{
		"neutral umbrella",
		"causal connectors",
		"one per independent thought",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("new_extraction.prompt missing rule phrase: %q", token)
		}
	}
}

// TestExtractionPromptsEnvelopeMetadata guards the Phase J Path A invariant — extraction
// prompts must NOT ask the LLM to populate envelope metadata fields (`requester`,
// `assigned_at`) because the platform adapter (Slack/Gmail/Notion/WhatsApp/Telegram)
// already resolves them from message metadata. Asking the LLM to extract envelope fields
// duplicates work, opens a fabrication path (LLM picks a body-mentioned name as sender),
// and conflicts with the user's design philosophy that WHO sent / WHEN sent / WHERE are
// platform-driven, not text-extracted. Each affected prompt must carry the
// "Envelope is metadata-driven" note as a stable marker.
func TestExtractionPromptsEnvelopeMetadata(t *testing.T) {
	t.Parallel()
	prompts := []string{
		"prompts/new_extraction.prompt",
		"prompts/gmail_system.prompt",
		"prompts/chat_system.prompt",
		"prompts/notion_system.prompt",
	}
	for _, p := range prompts {
		p := p
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			body := string(content)
			if !strings.Contains(body, "Envelope is metadata-driven") {
				t.Errorf("%s missing envelope-metadata note", p)
			}
			forbidden := []string{
				`"requester": "string"`,
				`"requester":"string"`,
				`"assigned_at": "string"`,
				`"assigned_at":"string"`,
			}
			for _, token := range forbidden {
				if strings.Contains(body, token) {
					t.Errorf("%s schema must not include envelope field token: %q", p, token)
				}
			}
		})
	}
}

// TestGmailAssigneeThirdPartyRule guards the gmail_system v1.5.0 invariant that the
// assignee rule explicitly handles third-party actor designation in reply bodies.
// Why: prior to v1.5.0 the prompt only declared "__CURRENT_USER__ for {{.CurrentUser}}",
// leaving AI to implicitly choose between sender and body-mentioned actor. Production
// regression: replies like "clarify with Andy" from {{.CurrentUser}} were assigned back
// to the sender. The third-party clause forces the explicit override even when the
// sender is the current user. If a future prompt revision drops this clause, the
// regression returns silently — this guard fails the build first.
func TestGmailAssigneeThirdPartyRule(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/gmail_system.prompt")
	if err != nil {
		t.Fatalf("read gmail_system: %v", err)
	}
	body := string(content)
	required := []string{
		"explicitly designates a third party as the actor",
		"even when the sender is {{.CurrentUser}}",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("gmail_system.prompt missing third-party assignee rule token: %q", token)
		}
	}
}

// TestNewExtractionBlankPolicy guards the v2.0.0 "leave blank if absent" core principle.
// Why: v2.0.0 dropped the v1.2.0 title-quality rules (MIN 30 chars, bare-verb rejection,
// action verb specificity) that contradicted the user's design philosophy of single-message
// 5W1H fidelity with downstream merge backfilling gaps. The "Core principle" sentence and
// the deadline-default-blank rule are the load-bearing invariants protecting this contract.
func TestNewExtractionBlankPolicy(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("prompts/new_extraction.prompt")
	if err != nil {
		t.Fatalf("read new_extraction: %v", err)
	}
	body := string(content)
	required := []string{
		"5W1H",
		"leave blank what it doesn't",
		"Never fabricate",
		"Vague cues",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Errorf("new_extraction.prompt missing v2.0.0 rule phrase: %q", token)
		}
	}
}
