//go:build regression

package ai_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// TestReportSummaryPrompt는 AI 모델이 report_summary.prompt의 지시사항을 정확히 따르는지 검증합니다.
// 실행 방법: go test -v -tags regression ./ai/...
func TestReportSummaryPrompt(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("Skipping LLM prompt test: GEMINI_API_KEY is not set")
	}

	tests := []struct {
		name           string
		inputLog       string
		expectedOutput []string // 결과물에 반드시 포함되어야 하는 문자열
		notExpected    []string // 결과물에 포함되어서는 안 되는 문자열
	}{
		{
			name: "Case 1: 일반적인 상황 (관리 공백 없음)",
			inputLog: `- [ ] 다음 분기 로드맵 회의 (From: 박철수 (Internal), To: 이민준 (Internal), Date: 04-03)
- [ ] 신규 API 명세 검토 (From: 이민준 (Internal), To: 최은서 (External), Date: 04-02)
- [V] v2.4.1 릴리즈 노트 초안 작성 (From: 김영희 (Internal), To: 박철수 (Internal), Date: 04-01)`,
			expectedOutput: []string{
				"## 1. Executive Summary",
				"## 2. Completed Tasks",
				"v2.4.1 릴리즈 노트",
				"## 3. Pending & Risks",
				"신규 API 명세 검토",
				"식별된 공백 없음", // 규칙: 병목이 없으면 명시해야 함
			},
			notExpected: []string{
				"David Kim", // 다른 컨텍스트의 내용이 환각(Hallucination)으로 나타나는지 방지
			},
		},
		{
			name:     "Case 2: 외부 파트너 지연으로 인한 관리 공백 발생",
			inputLog: `- [ ] 인증서 갱신 (4/5 만료). 2회 재요청에도 응답 없음. (From: 이민준 (Internal), To: David Kim (External), Date: 03-25)`,
			expectedOutput: []string{
				"## 4. [🚨 관리상의 공백 (Management Gap)]",
				"David Kim",
				"지연",
			},
			notExpected: []string{
				"식별된 공백 없음",
			},
		},
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
	if err != nil {
		t.Fatalf("AI 클라이언트 생성 실패: %v", err)
	}
	defer client.Close()

	promptBytes, err := os.ReadFile("prompts/report_summary.prompt")
	if err != nil {
		t.Fatalf("프롬프트 파일 읽기 실패: %v", err)
	}
	systemPrompt := string(promptBytes)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var result string

			// 캐시 키 생성: 프롬프트 + 입력 로그의 해시
			hashBytes := sha256.Sum256([]byte(systemPrompt + tc.inputLog))
			hashKey := hex.EncodeToString(hashBytes[:])
			cacheDir := filepath.Join("testdata", "prompt_cache")
			cachePath := filepath.Join(cacheDir, hashKey+".txt")

			if cachedData, err := os.ReadFile(cachePath); err == nil {
				// 캐시 히트: 로컬 파일 사용 (비용 $0)
				result = string(cachedData)
				t.Logf("캐시된 응답을 사용합니다 (API 호출 생략): %s", hashKey)
			} else {
				// 캐시 미스: 실제 AI 호출
				model := client.GenerativeModel("gemini-3-flash-preview")
				model.SystemInstruction = &genai.Content{
					Parts: []genai.Part{genai.Text(systemPrompt)},
				}

				resp, err := model.GenerateContent(ctx, genai.Text(tc.inputLog))
				if err != nil {
					t.Fatalf("AI API 호출 실패: %v", err)
				}
				if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
					t.Fatalf("AI 응답이 비어있습니다")
				}

				if part, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
					result = string(part)
					// 결과를 캐시에 저장하여 다음 테스트 시 비용 절감
					os.MkdirAll(cacheDir, 0755)
					os.WriteFile(cachePath, []byte(result), 0644)
				} else {
					t.Fatalf("예상치 못한 AI 응답 형식입니다: %v", resp.Candidates[0].Content.Parts[0])
				}
			}

			for _, expected := range tc.expectedOutput {
				if !strings.Contains(result, expected) {
					t.Errorf("예상되는 키워드가 누락되었습니다: %q\n실제 결과:\n%s", expected, result)
				}
			}

			for _, notExpected := range tc.notExpected {
				if strings.Contains(result, notExpected) {
					t.Errorf("출력되어서는 안 되는 키워드가 포함되었습니다: %q\n실제 결과:\n%s", notExpected, result)
				}
			}
		})
	}
}
