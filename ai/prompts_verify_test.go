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
		"Flag risks ONLY if supported by Evidence",
		"No anomalies detected.",
		"Preserve speaker stance",
		"No nominalization",
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

// TestNewExtractionNeutralUmbrella guards the v1.2.0 neutral-umbrella + paragraph-split rule.
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
			t.Errorf("new_extraction.prompt missing v1.2.0 rule phrase: %q", token)
		}
	}
}
