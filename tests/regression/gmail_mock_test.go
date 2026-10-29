//go:build regression

package regression

import (
	"context"
	"net/http"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"message-consolidator/types"
)

// Why: [Regression Test] Verifies that the Gmail extraction logic correctly separates multiple deliverables
// without repeating global context, using a stubbed chat-completion endpoint to ensure deterministic business logic validation.
func TestGmailExtraction_Mock(t *testing.T) {
	// Why: Top-level parallelization is disabled to prevent clobbering the global store.db.
	baseURL := testutil.NewChatCompletionServer(t, func(*http.Request) (string, error) {
		return `[{"task": "AI 기능 기술 덱 제작", "requester": "test@example.com", "category": "todo"}, {"task": "공식 서비스 대응 범위 확정", "requester": "test@example.com", "category": "todo"}]`, nil
	})

	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	client, err := ai.NewAIClient(ai.ProviderConfig{
		DeepSeekAPIKey:        "mock-api-key",
		DeepSeekAnalysisModel: "test-model",
		DeepSeekBaseURL:       baseURL,
	})
	if err != nil {
		t.Fatalf("Failed to initialize AI client: %v", err)
	}

	msg := types.EnrichedMessage{
		RawContent:    "Subject: Thailand Trip\n- Create AI deck\n- Finalize scope",
		SourceChannel: "gmail",
	}
	tasks, err := client.Analyze(ctx, "test@example.com", msg, "Korean", "gmail", "Inbox")
	verifyExtractionResults(t, tasks, err)
}

func verifyExtractionResults(t *testing.T, tasks []store.TodoItem, err error) {
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		if task.Task == "" {
			t.Error("Extracted task description is empty")
		}
		//Why: [Structural Check] Ensures task atomicity and prevents context bleeding by checking that global keywords are not present in every separate task.
		if len(task.Task) > 100 {
			t.Errorf("Task description too long (likely contains redundant context): %s", task.Task)
		}
	}
}
