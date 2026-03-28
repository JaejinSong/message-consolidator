package regression

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/store"

	"github.com/joho/godotenv"
)

// 이 테스트를 실행하려면 터미널에서 테스트용 API 키를 설정하거나 .env 파일에 정의해야 합니다.
// 예: export GEMINI_API_KEY_FOR_TEST="your_api_key" 또는 .env 에 GEMINI_API_KEY_FOR_TEST=... 추가
func TestAnalyze_Regression(t *testing.T) {
	t.Parallel()
	// .env 파일 로드 시도 (프로젝트 루트 탐색)
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("GEMINI_API_KEY_FOR_TEST")
	if apiKey == "" {
		t.Skip("Skipping regression test: GEMINI_API_KEY_FOR_TEST not set in environment or .env")
	}
	client, err := ai.NewGeminiClient(context.Background(), apiKey, "", "")
	if err != nil {
		t.Fatalf("Failed to create Gemini client: %v", err)
	}

	testCases, err := filepath.Glob("testdata/*_input.txt")
	if err != nil {
		t.Fatalf("Failed to find test cases: %v", err)
	}

	if len(testCases) == 0 {
		t.Fatalf("No test cases found in tests/regression/testdata/")
	}

	for _, testCasePath := range testCases {
		testCasePath := testCasePath
		baseName := strings.TrimSuffix(testCasePath, "_input.txt")
		testName := filepath.Base(baseName)

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			// 1. 입력 대화 내용 읽기
			inputBytes, err := os.ReadFile(testCasePath)
			if err != nil {
				t.Fatalf("Failed to read input file %s: %v", testCasePath, err)
			}

			// 2. 기대 결과 JSON 읽기
			expectedJSONPath := baseName + "_expected.json"
			expectedBytes, err := os.ReadFile(expectedJSONPath)
			if err != nil {
				t.Fatalf("Failed to read expected output file %s: %v", expectedJSONPath, err)
			}
			var expectedTasks []store.TodoItem
			if err := json.Unmarshal(expectedBytes, &expectedTasks); err != nil {
				t.Fatalf("Failed to unmarshal expected JSON: %v", err)
			}

			// 3. Analyze 함수 실제 호출
			// 언어 설정: _lang.txt 파일이 있으면 해당 값을 사용하고, 없으면 기대 결과에서 한글 여부로 판단합니다.
			lang := ""
			langPath := baseName + "_lang.txt"
			if langBytes, err := os.ReadFile(langPath); err == nil {
				lang = strings.TrimSpace(string(langBytes))
			} else {
				lang = "English"
				if containsKorean(string(expectedBytes)) {
					lang = "Korean"
				}
			}

			actualTasks, err := client.Analyze(context.Background(), "test.user@example.com", string(inputBytes), lang, "slack")
			if err != nil {
				t.Fatalf("Analyze function returned an error: %v", err)
			}

			// 4. 결과 비교 (Metadata는 유연하게, Task는 핵심 키워드 포함 여부로 검증)
			if len(expectedTasks) != len(actualTasks) {
				t.Fatalf("Count mismatch: want %d, got %d", len(expectedTasks), len(actualTasks))
			}

			for i := range expectedTasks {
				exp := expectedTasks[i]
				act := actualTasks[i]

				// Assignee 정규화 (me 와 실제 이메일은 동일하게 취급)
				normExpAssignee := strings.ToLower(exp.Assignee)
				normActAssignee := strings.ToLower(act.Assignee)
				if (normExpAssignee == "me" && normActAssignee == "test.user@example.com") ||
				   (normExpAssignee == "test.user@example.com" && normActAssignee == "me") {
					act.Assignee = exp.Assignee
				}

				// Metadata 검증
				if exp.Requester != act.Requester || exp.Assignee != act.Assignee || 
				   exp.Category != act.Category {
					t.Errorf("[%d] Metadata mismatch (Lang: %s):\nWant: %+v\nGot: %+v", i, lang, exp, act)
				}

				// Task 검증 (Keyword-based)
				expTask := strings.ToLower(exp.Task)
				actTask := strings.ToLower(act.Task)
				
				words := strings.Fields(expTask)
				var missingWords []string
				for _, w := range words {
					cleanW := strings.Trim(w, ".,!?;:()[]\"'")
					if len(cleanW) > 1 && !strings.Contains(actTask, cleanW) {
						missingWords = append(missingWords, cleanW)
					}
				}

				// 전체 키워드의 60% 이상 매칭되면 통과 (AI 비결정성 및 문장 구조 차이 고려)
				if len(words) > 0 {
					matchRate := float64(len(words)-len(missingWords)) / float64(len(words))
					if matchRate < 0.6 {
						t.Errorf("[%d] Task keyword match rate too low (%.2f): %d/%d missing. Missing list: %v\nWant: %s\nGot: %s", 
							i, matchRate, len(missingWords), len(words), missingWords, exp.Task, act.Task)
					}
				}
			}
		})
	}
}

func containsKorean(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}
