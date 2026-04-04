package store

import (
	"context"
	"message-consolidator/logger"
)

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions (new, update, resolve, cancel) to ensure consistency across different message sources (Slack, WhatsApp, Gmail).
func HandleTaskState(email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if item.State == "" {
		item.State = "new" // Default to new if state is missing
	}

	switch item.State {
	case "update":
		if item.ID != nil {
			UpdateTaskText(email, *item.ID, item.Task)
			if item.AssignedTo != "" {
				UpdateTaskAssignee(email, *item.ID, item.AssignedTo)
			}
			// Why: Appends the source of the triggering message to the existing task's source_channels to track cross-platform contributions.
			if existing, err := GetMessageByID(context.Background(), *item.ID); err == nil {
				merged := append(existing.SourceChannels, msg.Source)
				UpdateTaskSourceChannels(email, *item.ID, uniqueStrings(merged))
			}
			return *item.ID, nil
		}
		logger.Warnf("[ROUTER] Update state requested but no ID provided for %s", email)
	case "resolve":
		if item.ID != nil {
			err := MarkMessageDone(email, *item.ID, true)
			return *item.ID, err
		}
		logger.Warnf("[ROUTER] Resolve state requested but no ID provided for %s", email)
	case "cancel":
		if item.ID != nil {
			err := DeleteMessages(email, []int{*item.ID})
			return 0, err
		}
		logger.Warnf("[ROUTER] Cancel state requested but no ID provided for %s", email)
	case "new":
		_, id, err := SaveMessage(msg)
		return id, err
	default:
		logger.Warnf("[ROUTER] Unknown task state: %s", item.State)
	}
	
	// Fallback to SaveMessage if ID was missing for update/resolve/cancel
	_, id, err := SaveMessage(msg)
	return id, err
}

func uniqueStrings(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if entry != "" && !keys[entry] {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
