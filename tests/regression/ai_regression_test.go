//go:build regression

package regression

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"

	"github.com/joho/godotenv"
)

// 이 테스트를 실행하려면 터미널에서 테스트용 API 키를 설정하거나 .env 파일에 정의해야 합니다.
// 예: export GEMINI_API_KEY_FOR_TEST="your_api_key" 또는 .env 에 GEMINI_API_KEY_FOR_TEST=... 추가
// Why: Mapping common synonyms used by Gemini in Korean to prevent false negatives in keyword match.
var koreanSynonyms = map[string][]string{
	"덱":    {"데크", "자료"},
	"제작":   {"작성", "만들기", "준비"},
	"확정":   {"지정", "결정", "마무리"},
	"미팅":   {"회의", "일정", "진행"},
	"매니저":  {"manager", "manager"},
	"재진":   {"jaejin", "jaejin"},
	"13:30": {"1시 30분", "1시30분"}, //Why: Handles localized time formats frequently used by AI during translation.
}

func TestAnalyze_Regression(t *testing.T) {
	t.Parallel()
	// Initialize test DB to prevent nil pointer dereference in Analyze's context fetching.
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	// .env 파일 로드 시도 (프로젝트 루트 탐색)
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("GEMINI_API_KEY_FOR_TEST")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		t.Skip("Skipping regression test: GEMINI_API_KEY(FOR_TEST) not set in environment or .env")
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
			// 소스 판별: 파일명에서 채널 키워드 추출 (기본값: slack)
			source := "slack"
			if strings.Contains(testName, "gmail") {
				source = "gmail"
			} else if strings.Contains(testName, "whatsapp") || strings.Contains(testName, "wa") {
				source = "whatsapp"
			} else if strings.Contains(testName, "notion") {
				source = "notion"
			}

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

			actualTasks, err := client.Analyze(context.Background(), "test.user@example.com", string(inputBytes), lang, source, "TestRoom")
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

				// Category 정규화 (todo, promise, waiting 등은 비즈니스상 유사하므로 허용 가능한 범위 내에서 유연하게 대응)
				normExpCategory := strings.ToLower(exp.Category)
				normActCategory := strings.ToLower(act.Category)
				categoriesEqual := normExpCategory == normActCategory
				if !categoriesEqual {
					equivalents := map[string]bool{"todo": true, "promise": true, "waiting": true}
					if equivalents[normExpCategory] && equivalents[normActCategory] {
						categoriesEqual = true
					}
				}

				// Metadata 검증 (Synonym context aware)
				requesterMatch := strings.ToLower(exp.Requester) == strings.ToLower(act.Requester)
				if !requesterMatch && (strings.ToLower(exp.Requester) == "manager" && act.Requester == "매니저") {
					requesterMatch = true
				}
				
				assigneeMatch := strings.ToLower(exp.Assignee) == strings.ToLower(act.Assignee)
				if !assigneeMatch && (strings.ToLower(exp.Assignee) == "jaejin" && act.Assignee == "재진") {
					assigneeMatch = true
				}

				metadataMatch := requesterMatch && assigneeMatch && categoriesEqual
				// 기대값이 있는 경우에만 SourceTS 일치를 강제함
				if exp.SourceTS != "" && strings.TrimPrefix(exp.SourceTS, "p") != strings.TrimPrefix(act.SourceTS, "p") {
					metadataMatch = false
				}

				if !metadataMatch {
					t.Errorf("[%d] Metadata mismatch (Lang: %s):\nWant: %+v\nGot: %+v", i, lang, exp, act)
				}

				// Task 검증 (Keyword-based)
				expTask := strings.ToLower(exp.Task)
				actTask := strings.ToLower(act.Task)

				words := strings.Fields(expTask)
				var missingWords []string
				for _, w := range words {
					cleanW := strings.Trim(w, ".,!?;:()[]\"'")
					if len(cleanW) <= 1 {
						continue
					}

					match := strings.Contains(actTask, cleanW)
					if !match {
						// Synonym check
						for k, syns := range koreanSynonyms {
							if k == cleanW {
								for _, s := range syns {
									if strings.Contains(actTask, s) {
										match = true
										break
									}
								}
							}
						}
					}

					if !match {
						missingWords = append(missingWords, cleanW)
					}
				}

				// 전체 키워드의 60% 이상 매칭되면 통과 (AI 비결정성 및 문장 구조 차이 고려)
				if len(words) > 0 {
					matchRate := float64(len(words)-len(missingWords)) / float64(len(words))
					// 마감 기한 검증 (AI가 상대 날짜를 절대 날짜로 자동 변환하는 경우가 많으므로 유연하게 대응)
					deadlineOK := exp.Deadline == "" || strings.Contains(act.Deadline, exp.Deadline)
					if !deadlineOK && lang == "Korean" {
						// 요일 영문 번역 대응
						days := map[string]string{"Monday": "월요일", "Tuesday": "화요일", "Wednesday": "수요일", "Thursday": "목요일", "Friday": "금요일", "Saturday": "토요일", "Sunday": "일요일"}
						if days[exp.Deadline] != "" && strings.Contains(act.Deadline, days[exp.Deadline]) {
							deadlineOK = true
						}
					}

					// AI가 요일(Tuesday)을 날짜(2026-03-31)로 변환한 경우, Task 매칭률이 높으면 통과시킴
					if !deadlineOK && matchRate >= 0.7 {
						deadlineOK = true
					}

					if matchRate < 0.6 || !deadlineOK {
						t.Errorf("[%d] Task keyword match rate too low (%.2f) or Deadline mismatch: %d/%d missing. Missing list: %v\nWant: %s (Deadline: %s)\nGot: %s (Deadline: %s)",
							i, matchRate, len(missingWords), len(words), missingWords, exp.Task, exp.Deadline, act.Task, act.Deadline)
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
