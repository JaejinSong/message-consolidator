package ai

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/store"
)

// ExtractionContext는 템플릿 바인딩에 사용될 명시적 타입입니다.
// Why: [Type-Safe] 맵(map) 대신 구조체를 사용하여 컴파일 타임에 필드 유효성을 검증하고 런타임 오류를 방지합니다.
type ExtractionContext struct {
	MessagePayload      string
	CurrentTime         string
	Version             string
	Locale              string
	FewShots            []FewShot
	ExistingTasksJSON   string
	EnrichedMessageJSON string
	CurrentUser        string //Why: Explicitly identifies the host user to help AI distinguish between requester and assignee.
	CurrentUserEmail   string //Why: Provides the primary email of the user for strict identity mapping.
	CurrentUserID      int    //Why: Securely identifies the user for internal DB assignee mapping logic.
	ParentTask         string //Why: Context for completion/update evaluation threads.
}

// LimitFewShots는 최대 주입 가능한 예시 개수를 통제합니다.
// Why: [Token Economy] O(1) 슬라이싱으로 프롬프트 비대화를 방지합니다.
func (ctx *ExtractionContext) LimitFewShots(max int) {
	if max <= 0 || len(ctx.FewShots) <= max {
		return
	}
	ctx.FewShots = ctx.FewShots[:max]
}

// ExecutePrompt는 템플릿 렌더링과 AI 호출, 로깅을 조율합니다.
// Why: [Memory Efficient] bytes.Buffer를 사용하여 대규모 문자열 결합 시 메모리 재할당을 최소화하고 WhaTap 메모리 임계치를 보호합니다.
func ExecutePrompt(ctx context.Context, db *sql.DB, client *GeminiClient, prompt *ParsedPrompt, data ExtractionContext) (string, error) {
	// 1. 메모리 효율적인 템플릿 렌더링
	rendered, err := prompt.Render(data)
	if err != nil {
		return "", fmt.Errorf("prompt render error: %w", err)
	}

	// 2. Gemini API 호출
	aiResult, aiErr := client.CallGenericAPI(ctx, prompt.Meta.Model, rendered)

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
