package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

// MergeTasks consolidates multiple tasks into one using AI summarization for the title.
// Why: [Contextual Merge] Generates a representative English title from all merged messages.
func (s *TasksService) MergeTasks(ctx context.Context, email string, targetIDs []store.MessageID, destID store.MessageID) error {
	if destID <= 0 || len(targetIDs) == 0 {
		return fmt.Errorf("invalid merge parameters: targetCount=%d, destID=%d", len(targetIDs), destID)
	}
	allIDs := append([]store.MessageID{}, targetIDs...)
	allIDs = append(allIDs, destID)
	msgs, err := store.GetMessagesByIDs(ctx, store.GetDB(), email, allIDs)
	if err != nil {
		return err
	}

	var dest *store.ConsolidatedMessage
	var sources []store.ConsolidatedMessage
	for i := range msgs {
		if msgs[i].ID == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}
	if dest == nil {
		return fmt.Errorf("destination task not found")
	}

	// Why: [Reliability] AI summary is progressive; failures fallback to existing title.
	newTitle := s.generateSummaryTitle(ctx, email, dest, sources)
	return store.MergeTasksWithTitle(ctx, email, targetIDs, destID, newTitle)
}

func (s *TasksService) generateSummaryTitle(ctx context.Context, email string, dest *store.ConsolidatedMessage, sources []store.ConsolidatedMessage) string {
	inputs := []struct{ Title, Text string }{{dest.Task, dest.OriginalText}}
	for _, src := range sources {
		inputs = append(inputs, struct{ Title, Text string }{src.Task, src.OriginalText})
	}

	data, _ := json.Marshal(inputs)
	title, err := s.geminiClient.GenerateMergedTaskTitle(ctx, email, string(data))
	// Why: TrimSpace guard rejects whitespace-only AI output that would otherwise
	// pass `title != ""` and silently wipe the destination task title.
	if err == nil && strings.TrimSpace(title) != "" {
		return title
	}

	// Why: [Reliability] AI response blocks/timeouts fallback to simple title concatenation to prevent info loss.
	logger.Warnf("[TASKS] AI Merge Summary Failed (Error: %v). Falling back to concatenation.", err)
	titles := make([]string, 0, len(sources)+1)
	if t := strings.TrimSpace(dest.Task); t != "" {
		titles = append(titles, t)
	}
	for _, src := range sources {
		if t := strings.TrimSpace(src.Task); t != "" {
			titles = append(titles, t)
		}
	}
	// Why: All inputs blank → preserve dest.Task verbatim (caller's last-line guard
	// will reject if even that is empty). Never collapse to "" here.
	if len(titles) == 0 {
		return dest.Task
	}
	return s.truncateTitle(strings.Join(titles, " | "), 250)
}

func (s *TasksService) truncateTitle(t string, max int) string {
	if len(t) <= max {
		return t
	}
	return t[:max-3] + "..."
}

// ResolveProposals resolves extraction results against current active tasks.
// AI proposes (rawItems), Backend decides (returns refined items with correct IDs/States).
func (s *TasksService) ResolveProposals(ctx context.Context, email, room string, rawItems []store.TodoItem, active []store.ConsolidatedMessage) []store.TodoItem {
	var results []store.TodoItem
	for _, item := range rawItems {
		results = append(results, s.resolveProposalItem(room, item, active))
	}
	return results
}

// Why: Pulls the per-item match/state decision out of ResolveProposals so the loop body stays flat (≤3 nested levels).
func (s *TasksService) resolveProposalItem(room string, item store.TodoItem, active []store.ConsolidatedMessage) store.TodoItem {
	if match := s.findMatch(room, item, active); match != nil {
		item.ID = &match.ID
		// Upgrade 'new' to 'update' if we found an existing task.
		// Keep 'resolve', 'cancel', 'update' as AI intended.
		if item.State == "new" {
			item.State = "update"
		}
		return item
	}
	// Logic: If no match found, states requiring an ID must be downgraded.
	if item.State != "update" && item.State != "resolve" && item.State != "cancel" {
		return item
	}
	if item.Task != "" && item.State == "update" {
		item.State = "new" // Only 'update' can safely downgrade to 'new'
	} else {
		item.State = "none" // resolve/cancel with no match is dropped
	}
	return item
}

func (s *TasksService) findMatch(room string, item store.TodoItem, active []store.ConsolidatedMessage) *store.ConsolidatedMessage {
	// ID-first: AI explicitly identified the target task from existing context.
	if item.ID != nil && *item.ID != 0 {
		for i := range active {
			if active[i].ID == *item.ID {
				return &active[i]
			}
		}
	}

	for i := range active {
		m := &active[i]
		if m.Room != room || m.Category != item.Category {
			continue
		}
		// Why: prevent cross-thread merges in proposal resolution (mirrors isSemanticDup guard).
		if item.ThreadID != "" && m.ThreadID != "" && item.ThreadID != m.ThreadID {
			continue
		}

		sim := store.CalculateSimilarity(item.Task, m.Task)
		if sim >= 0.85 {
			return m
		}

		// Affinity Group Bonus: If AI group matches, we are more lenient (threshold 0.5)
		if hasAffinityMatch(m, item, sim) {
			return m
		}
	}
	return nil
}

// Why: Affinity-group lookup is a structural match path; isolate it so findMatch's main loop avoids deep nesting.
func hasAffinityMatch(m *store.ConsolidatedMessage, item store.TodoItem, sim float64) bool {
	if item.AffinityGroupID == "" || len(m.Metadata) == 0 || sim < 0.50 {
		return false
	}
	var meta struct {
		AffinityGroupID string `json:"affinity_group_id"`
	}
	if err := json.Unmarshal(m.Metadata, &meta); err != nil {
		return false
	}
	return meta.AffinityGroupID != "" && meta.AffinityGroupID == item.AffinityGroupID
}
