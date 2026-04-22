package scanner

import (
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"testing"
)

func TestIsFromMe(t *testing.T) {
	t.Parallel()
	user := store.User{Name: "Jaejin Song", Email: "jj@example.com"}

	tests := []struct {
		name string
		msg  types.RawMessage
		want bool
	}{
		{
			name: "IsFromMe flag set → true regardless of sender string",
			msg:  types.RawMessage{IsFromMe: true, Sender: "나"},
			want: true,
		},
		{
			name: "IsFromMe flag set with any sender → true",
			msg:  types.RawMessage{IsFromMe: true, Sender: "unknown"},
			want: true,
		},
		{
			name: "sender matches user name → true",
			msg:  types.RawMessage{Sender: "Jaejin Song"},
			want: true,
		},
		{
			name: "sender matches user email → true",
			msg:  types.RawMessage{Sender: "jj@example.com"},
			want: true,
		},
		{
			name: "sender is different person → false",
			msg:  types.RawMessage{Sender: "Hady"},
			want: false,
		},
		{
			name: "sender is empty, IsFromMe false → false",
			msg:  types.RawMessage{Sender: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isFromMe(tt.msg, user); got != tt.want {
				t.Errorf("isFromMe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildWAMetadataString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         types.RawMessage
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "RepliedToUser set → Reply-To tag present",
			msg:         types.RawMessage{RepliedToUser: "Hady"},
			wantContain: "Reply-To: Hady",
		},
		{
			name:       "RepliedToUser empty → no Reply-To tag",
			msg:        types.RawMessage{},
			wantAbsent: "Reply-To",
		},
		{
			name:        "Forwarded → tag present",
			msg:         types.RawMessage{IsForwarded: true},
			wantContain: "Forwarded",
		},
		{
			name:        "multiple tags combined",
			msg:         types.RawMessage{IsForwarded: true, RepliedToUser: "Hady"},
			wantContain: "Reply-To: Hady",
		},
		{
			name:        "attachment names included",
			msg:         types.RawMessage{AttachmentNames: []string{"report.pdf"}},
			wantContain: "report.pdf",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildWAMetadataString("test@example.com", tt.msg)
			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Errorf("buildWAMetadataString() = %q, want to contain %q", got, tt.wantContain)
			}
			if tt.wantAbsent != "" && strings.Contains(got, tt.wantAbsent) {
				t.Errorf("buildWAMetadataString() = %q, should NOT contain %q", got, tt.wantAbsent)
			}
		})
	}
}
