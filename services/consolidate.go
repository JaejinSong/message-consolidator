package services

import (
	"message-consolidator/store"
	"strings"
)

// MinConsolidationScore is the minimum affinity score required to merge two tasks.
const MinConsolidationScore int = 80

// ConsolidateTasks merges tasks sharing the same affinity group when their score meets the threshold.
// Tasks from the same source message (SourceTS) skip original_text append to prevent duplication.
func ConsolidateTasks(tasks []store.TodoItem) []store.TodoItem {
	if len(tasks) < 2 {
		return tasks
	}

	groups := make(map[string][]int)
	for i, t := range tasks {
		if t.AffinityGroupID == "" || t.AffinityScore < MinConsolidationScore {
			continue
		}
		groups[t.AffinityGroupID] = append(groups[t.AffinityGroupID], i)
	}

	merged := make([]bool, len(tasks))
	result := make([]store.TodoItem, 0, len(tasks))

	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		primary, secondary := pickConsolidationPrimary(tasks, indices)
		tasks[primary] = consolidateMergeInto(tasks[primary], tasks[secondary])
		merged[secondary] = true
	}

	for i, t := range tasks {
		if !merged[i] {
			result = append(result, t)
		}
	}
	return result
}

func pickConsolidationPrimary(tasks []store.TodoItem, indices []int) (primary, secondary int) {
	primary = indices[0]
	secondary = indices[1]
	if tasks[secondary].State == "update" && tasks[primary].State != "update" {
		return secondary, primary
	}
	return primary, secondary
}

// consolidateMergeInto combines secondary into primary with a timestamped separator.
// Note: original_text deduplication is handled at the DB layer via UpdateTaskFullAppend.
func consolidateMergeInto(primary, secondary store.TodoItem) store.TodoItem {
	secondaryContent := strings.TrimSpace(secondary.Task)
	if secondaryContent == "" || strings.Contains(primary.Task, secondaryContent) {
		return primary
	}
	date := strings.SplitN(secondary.AssignedAt, "T", 2)[0]
	if date == "" {
		date = secondary.SourceTS
	}
	var b strings.Builder
	b.WriteString(primary.Task)
	b.WriteString("\n\n--- [Update: ")
	b.WriteString(date)
	b.WriteString("] ---\n")
	b.WriteString(secondaryContent)
	primary.Task = b.String()
	return primary
}
