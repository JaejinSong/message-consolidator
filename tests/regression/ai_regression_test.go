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

// loadExistingTasks reads the optional "<case>_tasks.json" sidecar, which seeds the
// "Existing Tasks" context normally fetched from the DB — needed to exercise
// update/resolve state transitions against a matched open task without a live store.
func loadExistingTasks(path string) []store.ConsolidatedMessage {
	tasksPath := strings.TrimSuffix(path, "_input.txt") + "_tasks.json"
	b, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil
	}
	var tasks []store.ConsolidatedMessage
	if err := json.Unmarshal(b, &tasks); err != nil {
		return nil
	}
	return tasks
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
		ChatType:      loadChatType(path),
	}
	var err error
	if tasks := loadExistingTasks(path); tasks != nil {
		actual, err = client.AnalyzeWithContext(context.Background(), "test.user@example.com", msg, lang, source, "TestRoom", tasks)
	} else {
		actual, err = client.Analyze(context.Background(), "test.user@example.com", msg, lang, source, "TestRoom")
	}
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	return expected, actual
}

func runRegressionWithClient(t *testing.T, client *ai.AIClient, path, testName string) {
	expected, actual := analyzeCase(t, client, path, testName)
	compareResults(t, expected, actual)
}

// reportExtraction runs a case against a (live) provider and enforces majority-vote strictness:
// each case is analyzed up to 3 times and FAILS when the majority (2+) of runs diverge from the
// Gemini-recorded golden on the same field — a single-run outlier no longer masks a real prompt
// regression, but normal model non-determinism (one odd run out of three) still passes. An
// Analyze error or zero extraction on a task-present case always fails, in every run, regardless
// of mode.
//
// Set AI_REGRESSION_LENIENT=1 to restore the old log-only behavior (no field-level failures) for
// local debugging without tripping the deploy gate; this is documented, not silent — the chosen
// mode is always printed via t.Log.
func reportExtraction(t *testing.T, client *ai.AIClient, path, testName string) {
	if strings.TrimSpace(os.Getenv("AI_REGRESSION_LENIENT")) == "1" {
		t.Log("mode: lenient (AI_REGRESSION_LENIENT=1) — field divergences are logged, not failed")
		reportExtractionLenient(t, client, path, testName)
		return
	}
	t.Log("mode: strict majority-vote (2 of up to 3 runs decide pass/fail per field)")
	reportExtractionMajority(t, client, path, testName)
}

// reportExtractionLenient is the pre-majority-vote behavior: LOGS field-level divergences instead
// of failing, since exact field/count parity across providers is not by itself a correctness
// requirement (the goldens encode Gemini's output). Kept for local debugging via
// AI_REGRESSION_LENIENT=1.
func reportExtractionLenient(t *testing.T, client *ai.AIClient, path, testName string) {
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

// maxMajorityRuns bounds the vote: at most 3 live Analyze calls per case.
const maxMajorityRuns = 3

// runResult is one Analyze attempt's outcome against the golden, for majority-vote comparison.
type runResult struct {
	run   int
	pass  bool
	diffs []fieldDivergence
}

// reportExtractionMajority runs analyzeCase up to maxMajorityRuns times, stopping early when
// the first 2 runs already decide the outcome (both match the golden, or both diverge on the
// same field), and otherwise taking a 3-run majority vote.
func reportExtractionMajority(t *testing.T, client *ai.AIClient, path, testName string) {
	var results []runResult
	for i := 0; i < maxMajorityRuns; i++ {
		expected, actual := analyzeCase(t, client, path, testName)
		if len(expected) > 0 && len(actual) == 0 {
			t.Errorf("run %d: expected %d task(s) but provider extracted none", i+1, len(expected))
			return
		}
		pass, diffs := diffRun(expected, actual)
		results = append(results, runResult{run: i + 1, pass: pass, diffs: diffs})

		if len(results) != 2 {
			continue
		}
		if results[0].pass && results[1].pass {
			t.Logf("MAJORITY PASS (2/2 runs matched the golden, early stop)")
			return
		}
		if !results[0].pass && !results[1].pass && sameDivergenceShape(results[0].diffs, results[1].diffs) {
			failMajority(t, testName, results)
			return
		}
	}

	passVotes := 0
	for _, r := range results {
		if r.pass {
			passVotes++
		}
	}
	if passVotes >= 2 {
		t.Logf("MAJORITY PASS (%d/%d runs matched the golden)", passVotes, len(results))
		return
	}
	failMajority(t, testName, results)
}

// failMajority reports a majority-vote failure with a per-field diff.
func failMajority(t *testing.T, testName string, results []runResult) {
	t.Errorf("MAJORITY DIVERGENCE vs golden (%d/%d runs failed):\n  %s",
		countFailing(results), len(results), strings.Join(allNotes(results), "\n  "))
}

func countFailing(results []runResult) int {
	n := 0
	for _, r := range results {
		if !r.pass {
			n++
		}
	}
	return n
}

// allNotes collects labeled per-field diff notes from every failing run.
func allNotes(results []runResult) []string {
	var notes []string
	for _, r := range results {
		if r.pass {
			continue
		}
		for _, d := range r.diffs {
			notes = append(notes, fmt.Sprintf("run%d %s", r.run, d.note))
		}
	}
	return notes
}

// fieldDivergence records, per task index, which comparison categories diverged between a
// golden-expected and an actual TodoItem, plus a human-readable diff note.
type fieldDivergence struct {
	index   int
	fields  metadataFields
	content bool
	note    string
}

// diffRun is the majority-vote counterpart to matchResults: same tolerant comparison semantics
// (compareMetadata / verifyTaskContent), but returns per-field divergence detail instead of
// human notes only, so callers can compare failure *shape* (not exact values) across runs.
func diffRun(expected, actual []store.TodoItem) (pass bool, diffs []fieldDivergence) {
	if len(expected) != len(actual) {
		return false, []fieldDivergence{{
			index: -1,
			note:  fmt.Sprintf("count: want %d, got %d", len(expected), len(actual)),
		}}
	}
	pass = true
	for i := range expected {
		exp, act := expected[i], actual[i]
		normalizeAssignee(&exp, &act)
		fields := mismatchedMetadataFields(exp, act)
		// Why: title wording and deadline phrasing legitimately vary run to run ("Friday" vs
		// "금요일까지"), so content is reported but only metadata fields can fail the gate.
		contentMismatch := !verifyTaskContent(exp, act)
		if !fields.any() && !contentMismatch {
			continue
		}
		if fields.any() {
			pass = false
		}
		diffs = append(diffs, fieldDivergence{
			index:   i,
			fields:  fields,
			content: contentMismatch,
			note: fmt.Sprintf("[%d] req(exp=%q got=%q) cat(exp=%q got=%q) ts(exp=%q got=%q) state(exp=%q got=%q) assignee(exp=%q got=%q) task(exp=%q got=%q dl_exp=%q dl_got=%q)",
				i, exp.Requester, act.Requester, exp.Category, act.Category, exp.SourceTS, act.SourceTS,
				exp.State, act.State, exp.Assignee, act.Assignee, exp.Task, act.Task, exp.Deadline, act.Deadline),
		})
	}
	return pass, diffs
}

// sameDivergenceShape compares two runs' divergences by *category* (which fields diverged, at
// which indices) rather than by exact value — cross-run field-name agreement is what "2 runs
// diverge on the same field" means, not identical model output.
func sameDivergenceShape(a, b []fieldDivergence) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].index != b[i].index || a[i].fields != b[i].fields || a[i].content != b[i].content {
			return false
		}
	}
	return true
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
			notes = append(notes, fmt.Sprintf("[%d] metadata: exp{req=%q cat=%q ts=%q asn=%q} got{req=%q cat=%q ts=%q asn=%q}",
				i, exp.Requester, exp.Category, exp.SourceTS, exp.Assignee, act.Requester, act.Category, act.SourceTS, act.Assignee))
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

// metadataFields marks which compareMetadata categories diverged between an expected and an
// actual TodoItem. true means mismatch.
type metadataFields struct {
	requester bool
	category  bool
	ts        bool
	state     bool
	assignee  bool
}

func (f metadataFields) any() bool {
	return f.requester || f.category || f.ts || f.state || f.assignee
}

// mismatchedMetadataFields holds the same tolerant-comparison rules as compareMetadata but
// reports per-field results, so majority-vote diffing can compare failure *shape* across runs.
func mismatchedMetadataFields(exp, act store.TodoItem) metadataFields {
	// Why: requester is opt-in, like state/assignee below — every golden in testdata/ sets
	// requester to "", so without this guard majority-vote strictness would fail 100% of cases
	// on a field no fixture has ever actually asserted (pre-existing gap, only surfaced now that
	// mismatches are fatal instead of log-only).
	reqMatch := exp.Requester == "" || strings.EqualFold(exp.Requester, act.Requester)
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

	// Why: state is opt-in — most goldens predate the state/update/resolve schema fields, so
	// an empty exp.State must not force a comparison against them.
	stateMatch := exp.State == "" || strings.EqualFold(exp.State, act.State)

	// Why: assignee is opt-in — normalizeAssignee already ran on both sides (case-insensitive,
	// current-user aliases collapsed), so goldens without an assignee stay unaffected while
	// named-assignee cases (e.g. directed asks vs. FYI mentions) now get asserted.
	assigneeMatch := exp.Assignee == "" || strings.EqualFold(exp.Assignee, act.Assignee)
	// Why: a resolve/cancel closes the task, so who "owns" it in that reply carries no signal.
	if st := strings.ToLower(exp.State); st == "resolve" || st == "cancel" {
		assigneeMatch = true
	}

	return metadataFields{
		requester: !reqMatch,
		category:  !catMatch,
		ts:        !tsMatch,
		state:     !stateMatch,
		assignee:  !assigneeMatch,
	}
}

func compareMetadata(exp, act store.TodoItem) bool {
	return !mismatchedMetadataFields(exp, act).any()
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

// loadChatType reads an optional `<case>_chattype.txt` sidecar ("1to1" or "group"). Why:
// production passes ChatType to the model, and assignee rules for unaddressed asks depend on
// it; without it the regression model had to guess and flipped between runs.
func loadChatType(inputPath string) string {
	b, err := os.ReadFile(strings.TrimSuffix(inputPath, "_input.txt") + "_chattype.txt")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
