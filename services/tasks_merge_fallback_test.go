package services

import (
	"context"
	"message-consolidator/store"
	"strings"
	"testing"
)

type MockAI_Fail struct{}

func (m *MockAI_Fail) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestGenerateSummaryTitle_Fallback(t *testing.T) {
	s := &TasksService{
		geminiClient: &MockAI_Fail{},
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
