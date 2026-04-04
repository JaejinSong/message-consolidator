package ai

import (
	"sort"
	"strings"
)

// SelectFewShots는 사용자 쿼리(payload)와 가장 유사한 예시를 선택하여 반환합니다.
// Why: [RAG-like] 모든 예시를 주입하는 대신 쿼리 컨텍스트(채널, 키워드)가 일치하는 예시만 선택하여 토큰 효율성과 AI 응답 정확도를 동시에 확보합니다.
func SelectFewShots(payload string, examples []FewShot, limit int) []FewShot {
	if limit <= 0 || len(examples) == 0 {
		return nil
	}

	type ScoredShot struct {
		shot  FewShot
		score int
	}

	scored := make([]ScoredShot, len(examples))
	payloadLower := strings.ToLower(payload)

	for i, shot := range examples {
		scored[i] = ScoredShot{shot: shot, score: calculateScore(payloadLower, strings.ToLower(shot.Input))}
	}

	// 점수 기준 내림차순 정렬 (높은 점수가 앞으로)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 상위 n개 선택 (limit과 실제 개수 중 작은 값)
	resultCount := limit
	if len(scored) < limit {
		resultCount = len(scored)
	}

	final := make([]FewShot, resultCount)
	for i := 0; i < resultCount; i++ {
		final[i] = scored[i].shot
	}
	return final
}

// calculateScore는 간단한 키워드 매칭을 통해 유사도 점수를 계산합니다.
// Why: [Efficiency] 벡터 임베딩 없이도 채널 식별자(`Slack`, `Gmail` 등)와 주요 동작 키워드를 통해 컨텍스트를 빠르게 분류합니다.
func calculateScore(payload, input string) int {
	score := 0
	keywords := []string{"slack", "gmail", "whatsapp", "update", "finish", "deploy", "check"}
	for _, kw := range keywords {
		if strings.Contains(payload, kw) && strings.Contains(input, kw) {
			score++
		}
	}
	// 채널 식별자 가중치 (예: [ID:Slack...])
	if strings.Contains(payload, "id:") && strings.Contains(input, "id:") {
		score += 2
	}
	return score
}
