package services

import "testing"

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
