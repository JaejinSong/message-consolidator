package ai

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

