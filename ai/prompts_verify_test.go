package ai

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"text/template"
)

// TestPromptsNormalization은 모든 .prompt 파일의 규격을 검증합니다.
func TestPromptsNormalization(t *testing.T) {
	files, err := filepath.Glob("prompts/*.prompt")
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
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
	dummy := struct {
		MessagePayload string
		CurrentTime    string
		Locale         string
		FewShots       []struct {
			Input    string
			Expected string
		}
	}{
		MessagePayload: "dummy input",
		CurrentTime:    "2026-04-03 12:00:00",
		Locale:         "ko-KR",
		FewShots:       make([]struct{ Input, Expected string }, 0),
	}

	if err := tmpl.Execute(io.Discard, dummy); err != nil {
		t.Errorf("template execution failed: %v", err)
	}
}
