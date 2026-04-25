package services

import (
	"message-consolidator/store"
	"testing"
)

func TestResolveTaskTitle(t *testing.T) {
	tests := []struct {
		name     string
		aiTitle  string
		room     string
		original string
		want     string
	}{
		{
			name:    "ai title is substantive",
			aiTitle: "Review HeiTech Padu Berhad's latest projects",
			want:    "Review HeiTech Padu Berhad's latest projects",
		},
		{
			name:     "empty ai title falls back to gmail subject",
			aiTitle:  "",
			room:     "Gmail",
			original: "T: \"jjsong@whatap.io\" <jjsong@whatap.io>\nC:\nS: Onsite [Stream-Deves] : Present Observability Monitoring Tool (WhaTap)\nB:\nLocation: ...",
			want:     "Onsite [Stream-Deves] : Present Observability Monitoring Tool (WhaTap)",
		},
		{
			name:     "NONE sentinel falls back to original snippet",
			aiTitle:  "NONE",
			room:     "Slack",
			original: "Please check the production deployment status before EOD today.",
			want:     "Please check the production deployment status before EOD tod",
		},
		{
			name:     "short whitespace title falls back to original",
			aiTitle:  "   ",
			room:     "biz-global-tech",
			original: "Discuss BSI demo data preparation timeline",
			want:     "Discuss BSI demo data preparation timeline",
		},
		{
			name:     "missing original yields room marker",
			aiTitle:  "",
			room:     "[Temporary] WhaTap X Yokke POC",
			original: "",
			want:     "[[Temporary] WhaTap X Yokke POC]",
		},
		{
			name:     "everything empty falls to last-resort marker",
			aiTitle:  "",
			room:     "",
			original: "",
			want:     "[Unidentified message]",
		},
		{
			name:    "case-insensitive NONE detection",
			aiTitle: "none",
			room:    "channel-x",
			original: "Real content here that survives the filter.",
			want:    "Real content here that survives the filter.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTaskTitle(tt.aiTitle, tt.room, tt.original)
			if got != tt.want {
				t.Errorf("resolveTaskTitle(%q, %q, ...) = %q; want %q", tt.aiTitle, tt.room, got, tt.want)
			}
		})
	}
}

// Why (Phase J Path B / J-7): envelope (SenderRaw / SenderEmail) must outrank AI extraction
// for `requester`. AI is reachable only when both envelope sources are empty.
func TestResolveRequester_EnvelopeOverridesAI(t *testing.T) {
	tests := []struct {
		name           string
		aiRequester    string
		senderRaw      string
		senderEmail    string
		want           string
	}{
		{
			name:        "SenderRaw wins over AI",
			aiRequester: "Hallucinated Speaker",
			senderRaw:   "Alice",
			senderEmail: "alice@example.com",
			want:        "Alice",
		},
		{
			name:        "SenderEmail used when SenderRaw empty",
			aiRequester: "Hallucinated Speaker",
			senderRaw:   "",
			senderEmail: "alice@example.com",
			want:        "alice@example.com",
		},
		{
			name:        "AI fallback only when both envelope fields empty",
			aiRequester: "Last Resort",
			senderRaw:   "",
			senderEmail: "",
			want:        "Last Resort",
		},
		{
			name:        "All empty yields empty",
			aiRequester: "",
			senderRaw:   "",
			senderEmail: "",
			want:        "",
		},
		{
			name:        "AI 'unknown' sentinel falls through to empty",
			aiRequester: "unknown",
			senderRaw:   "",
			senderEmail: "",
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := TaskBuildParams{
				UserEmail:   "user@example.com",
				Item:        store.TodoItem{Requester: tt.aiRequester},
				SenderRaw:   tt.senderRaw,
				SenderEmail: tt.senderEmail,
			}
			got := resolveRequester(t.Context(), p)
			if got != tt.want {
				t.Errorf("resolveRequester() = %q; want %q", got, tt.want)
			}
		})
	}
}

// Why (Phase J Path B / J-7): chat_system Assignee rule 4 (`category=PROMISE → Sender`) was
// moved from the prompt to code. When AI leaves assignee blank and category is PROMISE, the
// envelope SenderRaw fills the slot.
func TestResolveAssignee_PromiseBranchUsesSenderRaw(t *testing.T) {
	tests := []struct {
		name      string
		aiAssign  string
		category  string
		senderRaw string
		want      string
	}{
		{
			name:      "PROMISE + empty AI → SenderRaw",
			aiAssign:  "",
			category:  "PROMISE",
			senderRaw: "Vy",
			want:      "Vy",
		},
		{
			name:      "PROMISE + AI 'unknown' sentinel → SenderRaw",
			aiAssign:  "unknown",
			category:  "PROMISE",
			senderRaw: "Vy",
			want:      "Vy",
		},
		{
			name:      "PROMISE + explicit AI assignee → AI wins (no override)",
			aiAssign:  "Bob",
			category:  "PROMISE",
			senderRaw: "Vy",
			want:      "Bob",
		},
		{
			name:      "Non-PROMISE + empty AI → AssigneeShared (existing behavior)",
			aiAssign:  "",
			category:  "TASK",
			senderRaw: "Vy",
			want:      AssigneeShared,
		},
		{
			name:      "PROMISE + empty AI + empty SenderRaw → AssigneeShared (no envelope speaker)",
			aiAssign:  "",
			category:  "PROMISE",
			senderRaw: "",
			want:      AssigneeShared,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := TaskBuildParams{
				UserEmail: "user@example.com",
				Item: store.TodoItem{
					Assignee: tt.aiAssign,
					Category: tt.category,
				},
				SenderRaw: tt.senderRaw,
			}
			got := resolveAssignee(t.Context(), p)
			if got != tt.want {
				t.Errorf("resolveAssignee() = %q; want %q", got, tt.want)
			}
		})
	}
}
