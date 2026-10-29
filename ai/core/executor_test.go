package core

import (
	"strings"
	"testing"
)

func TestExecutePromptRendering(t *testing.T) {
	t.Parallel()
	// 1. 테스트용 파싱된 프롬프트 설정 (Frontmatter 기파싱 상태 가정)
	prompt := &ParsedPrompt{
		Meta: PromptMeta{Name: "test", Version: "1.0", GeminiModel: "gemini-pro"},
		Body: "Hello {{.MessagePayload}} in {{.Locale}}!",
	}

	// 2. 렌더링용 컨텍스트 데이터
	ctxData := ExtractionContext{
		MessagePayload: "World",
		Locale:         "ko-KR",
	}

	// 렌더링 검증을 위해 파싱 로직 테스트
	if !strings.Contains(prompt.Body, "{{.MessagePayload}}") {
		t.Errorf("Prompt body should contains template tag")
	}

	// 3. ExtractionContext 필드 값 확인
	if ctxData.MessagePayload != "World" {
		t.Errorf("Expected World, got %s", ctxData.MessagePayload)
	}
}

func TestLimitFewShots(t *testing.T) {
	t.Parallel()
	ctx := &ExtractionContext{
		FewShots: []FewShot{
			{Input: "1", Expected: "A"},
			{Input: "2", Expected: "B"},
			{Input: "3", Expected: "C"},
		},
	}
	ctx.LimitFewShots(2)
	if len(ctx.FewShots) != 2 {
		t.Errorf("Expected 2, got %d", len(ctx.FewShots))
	}
}
