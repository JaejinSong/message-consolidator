package scanner

import (
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// groupMayTransitionTask reports whether any message in group plausibly resolves
// or updates one of the room's open tasks — either by replying to the task's
// originating message, or by carrying completion wording. Why: the group-level
// noise filter judges the whole batch as one unit, so a short reply that alone
// looks like noise ("Now okay", "Berikut capture nya") can silently drop a
// task's only resolution signal; this lets that signal skip the filter.
func groupMayTransitionTask(group []types.RawMessage, tasks []store.ConsolidatedMessage) bool {
	for _, msg := range group {
		if replyLinksToTask(msg, tasks) || services.HasCompletionSignal(msg.Text) {
			return true
		}
	}
	return false
}

// replyLinksToTask checks whether msg's reply-to id matches any open task's
// thread id, source timestamp id, or replied-to id.
func replyLinksToTask(msg types.RawMessage, tasks []store.ConsolidatedMessage) bool {
	if msg.ReplyToID == "" {
		return false
	}
	for _, task := range tasks {
		if msg.ReplyToID == task.ThreadID || msg.ReplyToID == task.SourceTS || msg.ReplyToID == task.RepliedToID {
			return true
		}
	}
	return false
}
