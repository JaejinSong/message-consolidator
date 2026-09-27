package scanner

import (
	"testing"

	"message-consolidator/store"
	"message-consolidator/types"
)

func TestGroupMayTransitionTask(t *testing.T) {
	tasks := []store.ConsolidatedMessage{
		{ThreadID: "thread-1", SourceTS: "ts-1", RepliedToID: "replied-1"},
	}
	cases := []struct {
		name  string
		group []types.RawMessage
		tasks []store.ConsolidatedMessage
		want  bool
	}{
		{
			name:  "reply-to matches task ThreadID",
			group: []types.RawMessage{{ReplyToID: "thread-1", Text: "hi"}},
			tasks: tasks,
			want:  true,
		},
		{
			name:  "reply-to matches task SourceTS",
			group: []types.RawMessage{{ReplyToID: "ts-1", Text: "hi"}},
			tasks: tasks,
			want:  true,
		},
		{
			name:  "unrelated reply id and no completion signal",
			group: []types.RawMessage{{ReplyToID: "other-id", Text: "any update?"}},
			tasks: tasks,
			want:  false,
		},
		{
			name:  "Indonesian completion phrase without reply link",
			group: []types.RawMessage{{Text: "Saat ini sudah bisa dimonitoring kembali"}},
			tasks: tasks,
			want:  true,
		},
		{
			name:  "English completion phrase without reply link",
			group: []types.RawMessage{{Text: "Now okay 👍😀"}},
			tasks: tasks,
			want:  true,
		},
		{
			name:  "plain message with no reply link and no signal",
			group: []types.RawMessage{{Text: "Berikut capture nya"}},
			tasks: tasks,
			want:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := groupMayTransitionTask(c.group, c.tasks); got != c.want {
				t.Errorf("groupMayTransitionTask() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestGroupMayTransitionTask_EmptyTasksHandledByCaller(t *testing.T) {
	// Why: processChannelGroup gates the call on len(tasks) > 0 before invoking
	// this helper — verify the helper itself is still safe with an empty slice.
	group := []types.RawMessage{{ReplyToID: "thread-1", Text: "hi"}}
	if got := groupMayTransitionTask(group, nil); got != false {
		t.Errorf("groupMayTransitionTask() with no tasks = %v, want false", got)
	}
}
