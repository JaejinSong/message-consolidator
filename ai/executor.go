package ai

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/store"
	"text/template"
)

// ExtractionContext는 템플릿 바인딩에 사용될 명시적 타입입니다.
// Why: [Type-Safe] 맵(map) 대신 구조체를 사용하여 컴파일 타임에 필드 유효성을 검증하고 런타임 오류를 방지합니다.
type ExtractionContext struct {
	MessagePayload string
	CurrentTime    string
	Locale         string
}

// ExecutePrompt는 템플릿 렌더링과 AI 호출, 로깅을 조율합니다.
// Why: [Memory Efficient] bytes.Buffer를 사용하여 대규모 문자열 결합 시 메모리 재할당을 최소화하고 WhaTap 메모리 임계치를 보호합니다.
func ExecutePrompt(ctx context.Context, db *sql.DB, client *GeminiClient, prompt *ParsedPrompt, data ExtractionContext) (string, error) {
	// Guard Clause: 템플릿 파싱 에러 처리
	tmpl, err := template.New("prompt").Parse(prompt.Body)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	// 1. 메모리 효율적인 템플릿 렌더링
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	// 2. Gemini API 호출
	aiResult, aiErr := client.CallGenericAPI(ctx, prompt.Meta.Model, buf.String())

	// 3. 실행 결과 로깅 (에러 발생 시에도 시도)
	status := "success"
	if aiErr != nil {
		status = "failed"
	}
	_ = store.LogPromptExecution(db, prompt.Meta.Name, prompt.Meta.Version, prompt.Meta.Model, status)

	if aiErr != nil {
		return "", fmt.Errorf("ai execution error: %w", aiErr)
	}
	return aiResult, nil
}
