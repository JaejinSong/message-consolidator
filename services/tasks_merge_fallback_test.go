package services

import (
	"context"
	"message-consolidator/store"
	"strings"
	"testing"
)

type MockAIFail struct{}

func (m *MockAIFail) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestGenerateSummaryTitle_Fallback(t *testing.T) {
	s := &TasksService{
		geminiClient: &MockAIFail{},
	}

	dest := &store.ConsolidatedMessage{Task: "Task A", OriginalText: "Text A"}
	sources := []store.ConsolidatedMessage{
		{Task: "Task B", OriginalText: "Text B"},
		{Task: "Task C", OriginalText: "Text C"},
	}

	result := s.generateSummaryTitle(context.Background(), "test@example.com", dest, sources)

	expected := "Task A | Task B | Task C"
	if result != expected {
		t.Errorf("Expected fallback title %q, got %q", expected, result)
	}
}

// MockAIBlank simulates an AI that returns whitespace-only output. The earlier
// guard (`title != ""`) accepted such strings and silently wiped the dest title.
type MockAIBlank struct{ payload string }

func (m *MockAIBlank) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	return m.payload, nil
}

// Regression: AI returns "" with no error → must not collapse to empty title.
// Reproduces the path that wiped row 11657.task during a 2-step merge sequence.
func TestGenerateSummaryTitle_AIReturnsEmpty_FallsBack(t *testing.T) {
	s := &TasksService{geminiClient: &MockAIBlank{payload: ""}}

	dest := &store.ConsolidatedMessage{Task: "Review", OriginalText: "X"}
	sources := []store.ConsolidatedMessage{
		{Task: "Register Hi Bank", OriginalText: "Y"},
		{Task: "Discuss POC", OriginalText: "Z"},
	}

	got := s.generateSummaryTitle(context.Background(), "test@example.com", dest, sources)
	want := "Review | Register Hi Bank | Discuss POC"
	if got != want {
		t.Errorf("empty-AI fallback: want %q, got %q", want, got)
	}
}

// Regression: AI returns whitespace-only ("   ", "\n") — must be rejected too.
func TestGenerateSummaryTitle_AIReturnsWhitespace_FallsBack(t *testing.T) {
	for _, payload := range []string{"   ", "\n", "\t\t", " \n "} {
		s := &TasksService{geminiClient: &MockAIBlank{payload: payload}}
		dest := &store.ConsolidatedMessage{Task: "Review"}
		sources := []store.ConsolidatedMessage{{Task: "Discuss POC"}}

		got := s.generateSummaryTitle(context.Background(), "test@example.com", dest, sources)
		if strings.TrimSpace(got) == "" {
			t.Errorf("payload=%q: title collapsed to whitespace %q", payload, got)
		}
	}
}

// All inputs blank → returns dest.Task verbatim (preserves whatever was there
// rather than producing "" or " | "). The store-layer guard then rejects.
func TestGenerateSummaryTitle_AllBlankSources_PreservesDest(t *testing.T) {
	s := &TasksService{geminiClient: &MockAIFail{}}
	dest := &store.ConsolidatedMessage{Task: "Original Title"}
	sources := []store.ConsolidatedMessage{{Task: ""}, {Task: "  "}}

	got := s.generateSummaryTitle(context.Background(), "test@example.com", dest, sources)
	if got != "Original Title" {
		t.Errorf("expected dest.Task preserved, got %q", got)
	}
}

func TestGenerateSummaryTitle_Truncation(t *testing.T) {
	s := &TasksService{}
	
	longTitle := strings.Repeat("A", 300)
	result := s.truncateTitle(longTitle, 10)
	
	if len(result) != 10 {
		t.Errorf("Expected truncated length 10, got %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("Expected '...' suffix, got %q", result)
	}
}
