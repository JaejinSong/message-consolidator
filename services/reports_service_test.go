package services

import (
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

func TestReportsService_CalculateGraph(t *testing.T) {
	svc := &ReportsService{}
	messages := []store.ConsolidatedMessage{
		{Requester: "Alice", Assignee: "JJ", Source: "slack"},
		{Requester: "Alice", Assignee: "JJ", Source: "slack"},
		{Requester: "Bob", Assignee: "JJ", Source: "whatsapp"},
		{Requester: "JJ", Assignee: "Alice", Source: "slack"},
	}

	graphData := svc.generateVisualizationData(messages)

	// Verify nodes
	foundAlice := false
	foundJJ := false
	for _, n := range graphData.Nodes {
		if n.ID == "Alice" {
			foundAlice = true
			if n.Value != 3 { // Requester 2 + Assignee 1
				t.Errorf("Alice value expected 3, got %f", n.Value)
			}
		}
		if n.ID == "JJ" {
			foundJJ = true
			if n.Value != 4 { // Assignee 3 + Requester 1
				t.Errorf("JJ value expected 4, got %f", n.Value)
			}
		}
	}

	if !foundAlice || !foundJJ {
		t.Errorf("Alice or JJ node missing")
	}

	// Verify edges
	if len(graphData.Edges) < 1 {
		t.Errorf("Edges should not be empty")
	}
}

func TestReportsService_TruncatePayload(t *testing.T) {
	svc := &ReportsService{}
	messages := []store.ConsolidatedMessage{}
	for i := 0; i < 50; i++ {
		messages = append(messages, store.ConsolidatedMessage{
			Task:      "Test Task " + strings.Repeat("a", 100),
			Requester: "Sender",
			Assignee:  "Receiver",
			CreatedAt: time.Now(),
		})
	}

	summary := svc.prepareTaskSummaryForAI(messages, 1000)
	if len([]byte(summary)) > 1000 {
		t.Errorf("Summary too long: %d bytes (limit 1000)", len([]byte(summary)))
	}
}

func TestReportsService_TruncatePriority(t *testing.T) {
	svc := &ReportsService{}
	now := time.Now()

	// 1 old, incomplete task
	oldIncomplete := store.ConsolidatedMessage{
		Task:      "URGENT OLD TASK",
		Requester: "System",
		Assignee:  "User",
		Done:      false,
		CreatedAt: now.Add(-48 * time.Hour),
	}

	messages := []store.ConsolidatedMessage{oldIncomplete}

	// 20 new, completed tasks (these should be truncated if limit is small)
	for i := 0; i < 20; i++ {
		messages = append(messages, store.ConsolidatedMessage{
			Task:      "Completed Task " + strings.Repeat("a", 100),
			Requester: "Sender",
			Assignee:  "Receiver",
			Done:      true,
			CreatedAt: now.Add(time.Duration(-i) * time.Hour),
		})
	}

	// Set limit to only allow about 2-3 lines
	summary := svc.prepareTaskSummaryForAI(messages, 300)

	if !strings.Contains(summary, "URGENT OLD TASK") {
		t.Errorf("Critical incomplete old task was truncated, but it should have been prioritized")
	}

	if !strings.Contains(summary, "- [ ] URGENT OLD TASK") {
		t.Errorf("Task status mark should be [ ]; got summary: %s", summary)
	}
}
