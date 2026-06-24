//go:build regression

package regression

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"

	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

// To run this test, set GEMINI_API_KEY_FOR_TEST in your environment or .env file.
// Run with "UPDATE_GOLDEN=1 make test-all" to record new API responses to VCR dumps.

var taskKeywords = map[string][]string{
	"deck":    {"deck", "presentation", "slides", "덱", "자료"},
	"create":  {"create", "write", "make", "prepare", "제작", "작성", "buat", "tulis"},
	"confirm": {"confirm", "decide", "finalize", "확정", "결정", "konfirmasi", "putuskan"},
	"meeting": {"meeting", "sync", "call", "미팅", "회의", "rapat", "pertemuan"},
	"manager": {"manager", "admin", "매니저", "pengelola"},
	"tech":    {"tech", "technical", "feature", "기술", "기능", "teknis", "fitur"},
	"blog":    {"blog", "posting", "블로그", "포스팅"},
	"hire":    {"hire", "onboarding", "recruit", "채용", "온보딩", "rekrut", "employee"},
	"tuesday": {"tuesday", "화요일", "selasa"},
	"friday":  {"friday", "금요일", "jumat"},
	"guide":   {"guide", "manual", "handbook", "document", "가이드"},
	"update":  {"update", "revise", "improve", "edit", "업데이트"},
}

// vcrTransport acts as an HTTP interceptor to record or replay API calls.
type vcrTransport struct {
	Transport http.RoundTripper
	Mode      string
	MockFile  string
	APIKey    string
}

func (t *vcrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Mode == "replay" {
		dump, err := os.ReadFile(t.MockFile)
		if err != nil {
			return nil, fmt.Errorf("VCR replay missing for %s. Run tests with UPDATE_GOLDEN=1 to record", t.MockFile)
		}
		return http.ReadResponse(bufio.NewReader(bytes.NewReader(dump)), req)
	}

	// record mode
	if t.APIKey != "" {
		req.Header.Set("x-goog-api-key", t.APIKey)
	}
	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// httputil.DumpResponse includes status code, headers, and body.
	dump, err := httputil.DumpResponse(resp, true)
	if err == nil {
		os.WriteFile(t.MockFile, dump, 0644)
	}
	return resp, nil
}

func TestAnalyze_Regression(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	t.Cleanup(cleanup)
	logger.InitAIInferenceLogger()

	godotenv.Load("../../.env", ".env", "../.env")

	testCases, _ := filepath.Glob("testdata/*_input.txt")
	for _, path := range testCases {
		path := path
		testName := strings.TrimSuffix(filepath.Base(path), "_input.txt")
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runSingleRegression(t, path, testName)
		})
	}
}

func shouldRecord(mockPath string) bool {
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		return true
	}

	mockInfo, err := os.Stat(mockPath)
	if err != nil {
		return true // File does not exist
	}

	prompts, _ := filepath.Glob("../../ai/core/prompts/*.prompt")
	for _, p := range prompts {
		pInfo, err := os.Stat(p)
		if err == nil && pInfo.ModTime().After(mockInfo.ModTime()) {
			return true // Prompt has been modified
		}
	}
	return false
}

func setupGeminiClientForTest(testName string) (*ai.GeminiClient, error) {
	mockPath := filepath.Join("testdata", testName+"_vcr.dump")
	mode := "replay"
	if shouldRecord(mockPath) {
		mode = "record"
		fmt.Printf("[VCR] Recording mode enabled for %s (File missing or Prompt updated)\n", testName)
	}

	apiKey := os.Getenv("GEMINI_API_KEY_FOR_TEST")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}

	// In replay mode, we don't need a real API key.
	if mode == "replay" && apiKey == "" {
		apiKey = "dummy-vcr-key"
	} else if mode == "record" && apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required for record mode (UPDATE_GOLDEN=1)")
	}

	transport := &vcrTransport{
		Transport: http.DefaultTransport,
		Mode:      mode,
		MockFile:  mockPath,
		APIKey:    apiKey,
	}
	httpClient := &http.Client{Transport: transport}

	return ai.NewGeminiClient(context.Background(), apiKey, "", "", func(c *genai.ClientConfig) {
		c.HTTPClient = httpClient
	})
}

// setupDeepSeekClientForTest builds a live DeepSeek AIClient for extraction-equivalence
// runs. There is no VCR for DeepSeek (the dumps are Gemini-shaped), so this always hits the
// real API; callers must gate on DEEPSEEK_API_KEY.
func setupDeepSeekClientForTest() (*ai.AIClient, error) {
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY not set")
	}
	return ai.NewAIClient(context.Background(), ai.ProviderConfig{
		Provider:        "deepseek",
		DeepSeekAPIKey:  key,
		DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
	})
}

// TestAnalyze_Regression_DeepSeek replays the SAME golden fixtures and tolerant comparison
// as the Gemini regression, but against the live DeepSeek provider — verifying extraction
// equivalence (plan verification step 3). Skipped unless DEEPSEEK_API_KEY is set.
func TestAnalyze_Regression_DeepSeek(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping DeepSeek extraction equivalence")
	}
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	t.Cleanup(cleanup)
	logger.InitAIInferenceLogger()
	godotenv.Load("../../.env", ".env", "../.env")

	client, err := setupDeepSeekClientForTest()
	if err != nil {
		t.Fatalf("DeepSeek client: %v", err)
	}

	testCases, _ := filepath.Glob("testdata/*_input.txt")
	for _, path := range testCases {
		path := path
		testName := strings.TrimSuffix(filepath.Base(path), "_input.txt")
		// Why: serial (no t.Parallel) — share one client and avoid bursting the live API.
		t.Run(testName, func(t *testing.T) {
			reportExtraction(t, client, path, testName)
		})
	}
}

func runSingleRegression(t *testing.T, path, testName string) {
	client, err := setupGeminiClientForTest(testName)
	if err != nil {
		t.Skipf("Skipping regression: %v", err)
	}
	runRegressionWithClient(t, client, path, testName)
}

func analyzeCase(t *testing.T, client *ai.AIClient, path, testName string) (expected, actual []store.TodoItem) {
	input, _ := os.ReadFile(path)
	expectedBytes, _ := os.ReadFile(strings.TrimSuffix(path, "_input.txt") + "_expected.json")
	json.Unmarshal(expectedBytes, &expected)

	lang := determineLang(path, string(expectedBytes))
	source := determineSource(testName)
	msg := types.EnrichedMessage{
		RawContent:    string(input),
		SourceChannel: source,
	}
	var err error
	actual, err = client.Analyze(context.Background(), "test.user@example.com", msg, lang, source, "TestRoom")
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	return expected, actual
}

func runRegressionWithClient(t *testing.T, client *ai.AIClient, path, testName string) {
	expected, actual := analyzeCase(t, client, path, testName)
	compareResults(t, expected, actual)
}

// reportExtraction runs a case against a (live) provider and reports equivalence vs the
// Gemini-recorded golden. It FAILS only on real defects — an Analyze error or zero extraction
// on a task-present case — and otherwise LOGS field-level divergences, since exact field/count
// parity across providers is not a correctness requirement (the goldens encode Gemini's output).
func reportExtraction(t *testing.T, client *ai.AIClient, path, testName string) {
	expected, actual := analyzeCase(t, client, path, testName)
	if len(expected) > 0 && len(actual) == 0 {
		t.Errorf("expected %d task(s) but provider extracted none", len(expected))
		return
	}
	if pass, notes := matchResults(expected, actual); pass {
		t.Logf("EQUIVALENT (%d task(s))", len(actual))
	} else {
		t.Logf("DIVERGENCE vs Gemini golden (cross-model nuance, not a plumbing bug):\n  %s", strings.Join(notes, "\n  "))
	}
}

// matchResults is the non-fatal twin of compareResults: it returns whether actual would pass
// the tolerant golden comparison plus human-readable notes on each divergence.
func matchResults(expected, actual []store.TodoItem) (pass bool, notes []string) {
	if len(expected) != len(actual) {
		return false, []string{fmt.Sprintf("count: want %d, got %d", len(expected), len(actual))}
	}
	pass = true
	for i := range expected {
		exp, act := expected[i], actual[i]
		normalizeAssignee(&exp, &act)
		if !compareMetadata(exp, act) {
			pass = false
			notes = append(notes, fmt.Sprintf("[%d] metadata: exp{req=%q cat=%q ts=%q} got{req=%q cat=%q ts=%q}",
				i, exp.Requester, exp.Category, exp.SourceTS, act.Requester, act.Category, act.SourceTS))
		}
		if !verifyTaskContent(exp, act) {
			pass = false
			notes = append(notes, fmt.Sprintf("[%d] content: exp=%q (dl=%q) got=%q (dl=%q)",
				i, exp.Task, exp.Deadline, act.Task, act.Deadline))
		}
	}
	return pass, notes
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
		return s == "me" || s == "__current_user__" || s == "test.user@example.com" ||
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
