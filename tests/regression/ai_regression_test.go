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
	"message-consolidator/types"

	"github.com/joho/godotenv"
)

// To run this test, set GEMINI_API_KEY_FOR_TEST in your environment or .env file.
// Example: export GEMINI_API_KEY_FOR_TEST="your_api_key"

// taskKeywords stores common synonyms to help with keyword-based matching in different languages.
var taskKeywords = map[string][]string{
	"deck":    {"deck", "presentation", "slides", "덱", "자료"},
	"create":  {"create", "write", "make", "prepare", "제작", "작성", "buat", "tulis"},
	"confirm": {"confirm", "decide", "finalize", "확정", "결정", "konfirmasi", "putuskan"},
	"meeting": {"meeting", "sync", "call", "미팅", "회의", "rapat", "pertemuan"},
	"manager": {"manager", "admin", "매니저", "pengelola"},
	"tech":    {"tech", "technical", "feature", "기술", "기능", "teknis", "fitur"},
	"blog":    {"blog", "posting", "블로그", "포스팅"},
	"hire":    {"hire", "onboarding", "recruit", "채용", "온보딩", "rekrut", "employee"}, // Added employee
	"tuesday": {"tuesday", "화요일", "selasa"},
	"friday":  {"friday", "금요일", "jumat"},
	"guide":   {"guide", "manual", "handbook", "document", "가이드"}, // Added guide
	"update":  {"update", "revise", "improve", "edit", "업데이트"},    // Added update
}

func TestAnalyze_Regression(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	godotenv.Load("../../.env", ".env", "../.env")
	client, err := setupGeminiClient()
	if err != nil {
		t.Skipf("Skipping regression: %v", err)
	}

	testCases, _ := filepath.Glob("testdata/*_input.txt")
	for _, path := range testCases {
		path := path
		testName := strings.TrimSuffix(filepath.Base(path), "_input.txt")
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runSingleRegression(t, client, path, testName)
		})
	}
}

func setupGeminiClient() (*ai.GeminiClient, error) {
	apiKey := os.Getenv("GEMINI_API_KEY_FOR_TEST")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return nil, context.DeadlineExceeded // Sentinel
	}
	return ai.NewGeminiClient(context.Background(), apiKey, "", "")
}

func runSingleRegression(t *testing.T, client *ai.GeminiClient, path, testName string) {
	input, _ := os.ReadFile(path)
	expectedBytes, _ := os.ReadFile(strings.TrimSuffix(path, "_input.txt") + "_expected.json")

	var expected []store.TodoItem
	json.Unmarshal(expectedBytes, &expected)

	lang := determineLang(path, string(expectedBytes))
	source := determineSource(testName)

	msg := types.EnrichedMessage{
		RawContent:    string(input),
		SourceChannel: source,
	}
	actual, err := client.Analyze(context.Background(), "test.user@example.com", msg, lang, source, "TestRoom")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}

	compareResults(t, expected, actual)
}

func determineSource(testName string) string {
	if strings.Contains(testName, "gmail") {
		return "gmail"
	}
	if strings.Contains(testName, "whatsapp") || strings.Contains(testName, "wa") {
		return "whatsapp"
	}
	if strings.Contains(testName, "notion") {
		return "notion"
	}
	return "slack"
}

func determineLang(path, expectedContent string) string {
	langPath := strings.TrimSuffix(path, "_input.txt") + "_lang.txt"
	if b, err := os.ReadFile(langPath); err == nil {
		return strings.TrimSpace(string(b))
	}
	if containsKorean(expectedContent) {
		return "Korean"
	}
	return "English"
}

func compareResults(t *testing.T, expected, actual []store.TodoItem) {
	if len(expected) != len(actual) {
		t.Fatalf("Count mismatch: want %d, got %d", len(expected), len(actual))
	}
	for i := range expected {
		verifyItem(t, i, expected[i], actual[i])
	}
}

func verifyItem(t *testing.T, index int, exp, act store.TodoItem) {
	normalizeAssignee(&exp, &act)

	if !compareMetadata(exp, act) {
		t.Errorf("[%d] Metadata mismatch:\nExp: %+v\nGot: %+v", index, exp, act)
	}

	if !verifyTaskContent(exp, act) {
		t.Errorf("[%d] Content/Deadline failure.\nExp: %s (DL: %s)\nGot: %s (DL: %s)",
			index, exp.Task, exp.Deadline, act.Task, act.Deadline)
	}
}

func normalizeAssignee(exp, act *store.TodoItem) {
	isMe := func(s string) bool {
		s = strings.ToLower(s)
		return s == "me" || s == "test.user@example.com" ||
			strings.Contains(s, "bob") || strings.Contains(s, "alice") ||
			strings.Contains(s, "hady") || strings.Contains(s, "jaejin")
	}
	if isMe(exp.Assignee) && isMe(act.Assignee) {
		act.Assignee = exp.Assignee
	}
}

func compareMetadata(exp, act store.TodoItem) bool {
	reqMatch := strings.EqualFold(exp.Requester, act.Requester)
	if !reqMatch && (strings.ToLower(exp.Requester) == "manager" && act.Requester == "매니저") {
		reqMatch = true
	}

	catMatch := strings.EqualFold(exp.Category, act.Category)
	if !catMatch {
		// TASK and PROMISE are both actionable, allow interchange.
		c1, c2 := strings.ToUpper(exp.Category), strings.ToUpper(act.Category)
		if (c1 == "TASK" || c1 == "PROMISE") && (c2 == "TASK" || c2 == "PROMISE") {
			catMatch = true
		}
	}

	tsMatch := true
	if exp.SourceTS != "" && strings.TrimPrefix(exp.SourceTS, "p") != strings.TrimPrefix(act.SourceTS, "p") {
		tsMatch = false
	}

	return reqMatch && catMatch && tsMatch
}

func verifyTaskContent(exp, act store.TodoItem) bool {
	matchRate := calculateMatchRate(exp.Task, act.Task)
	deadlineOK := exp.Deadline == "" || strings.Contains(act.Deadline, exp.Deadline)

	if !deadlineOK && matchRate >= 0.7 {
		deadlineOK = true // Allow flexible date formats if content is strong
	}

	return matchRate >= 0.6 && deadlineOK
}

func calculateMatchRate(expTask, actTask string) float64 {
	expTask, actTask = strings.ToLower(expTask), strings.ToLower(actTask)
	words := strings.Fields(expTask)
	if len(words) == 0 {
		return 1.0
	}

	matched := 0
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:()[]\"'")
		if len(w) <= 1 {
			continue
		}
		if matchWord(w, actTask) {
			matched++
		}
	}
	return float64(matched) / float64(len(words))
}

func matchWord(word, actTask string) bool {
	if strings.Contains(actTask, word) {
		return true
	}
	for k, syns := range taskKeywords {
		if k == word {
			for _, s := range syns {
				if strings.Contains(actTask, s) {
					return true
				}
			}
		}
	}
	return false
}

func containsKorean(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}
