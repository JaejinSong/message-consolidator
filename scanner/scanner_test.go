package scanner

import (
	"message-consolidator/store"
	"testing"
)

func TestGetEffectiveAliases(t *testing.T) {
	tests := []struct {
		name    string
		user    store.User
		aliases []string
		want    []string
	}{
		{
			name:    "should combine name and email prefix as aliases",
			user:    store.User{Email: "jjsong@whatap.io", Name: "Jaejin Song"},
			aliases: []string{"JJ", "송재진"},
			want:    []string{"JJ", "송재진", "Jaejin Song", "jjsong"},
		},
		{
			name:    "should de-duplicate aliases when there is partial overlap",
			user:    store.User{Email: "jjsong@whatap.io", Name: "jjsong"},
			aliases: []string{"JJ", "jjsong"},
			want:    []string{"JJ", "jjsong"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEffectiveAliases(tt.user, tt.aliases)
			if len(got) != len(tt.want) {
				t.Errorf("got %d aliases, want %d", len(got), len(tt.want))
			}
			gotMap := make(map[string]bool)
			for _, a := range got {
				gotMap[a] = true
			}
			for _, w := range tt.want {
				if !gotMap[w] {
					t.Errorf("missing expected alias: %s", w)
				}
			}
		})
	}
}

func TestIsAliasMatched(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		sender   string
		alias    string
		expected bool
	}{
		{"should return true when sender name exactly matches the alias", "Hello", "JJ", "JJ", true},
		{"should return true when sender name contains the alias", "Hello", "Jaejin Song (JJ)", "JJ", true},
		{"should return true when text contains the exact long alias", "이건 송재진 님이 처리해주세요.", "System", "송재진", true},
		{"should return true when text contains a short Korean alias as a prefix", "나는 이 일을 할게요.", "System", "나", true},
		{"should return true when text exactly matches the short Korean alias", "나 이거 할게", "System", "나", true},
		// Why: '나' matches a syllable inside '지나가다가', but should be strictly ignored to prevent aggressive false positives.
		{"should return false when a short alias is embedded inside a word", "지나가다가 봤습니다.", "System", "나", false},
		{"should return false when neither sender nor text matches the alias", "Please review", "John", "JJ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAliasMatched(tt.text, tt.sender, tt.alias)
			if got != tt.expected {
				t.Errorf("IsAliasMatched() = %v, want %v (Text: %q, Sender: %q, Alias: %q)", got, tt.expected, tt.text, tt.sender, tt.alias)
			}
		})
	}
}

// mockSlackResolver provides a controlled environment for testing Slack mention resolutions
// without requiring an actual connection to the external Slack API.
type mockSlackResolver struct {
	users map[string]string
}

func (m mockSlackResolver) GetUserName(userID string) string {
	return m.users[userID] // Why: Simulates the actual API behavior where an unknown ID yields an empty string.
}

func TestResolveSlackMentions(t *testing.T) {
	mockResolver := mockSlackResolver{
		users: map[string]string{
			"U0208BU06JE": "Jaejin Song",
			"U12345678":   "John Doe",
		},
	}

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "should replace a single Slack user ID with their real name",
			text:     "Hi <@U0208BU06JE>, for .net agent installation in linux, is it possible?",
			expected: "Hi @Jaejin Song, for .net agent installation in linux, is it possible?",
		},
		{
			name:     "should replace multiple Slack user IDs in a single message",
			text:     "<@U0208BU06JE> and <@U12345678> are assigned to this task.",
			expected: "@Jaejin Song and @John Doe are assigned to this task.",
		},
		{
			name:     "should leave the mention tag unchanged if the user ID is unknown",
			text:     "Hello <@U99999999>, are you there?",
			expected: "Hello <@U99999999>, are you there?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSlackMentions(tt.text, mockResolver)
			if got != tt.expected {
				t.Errorf("resolveSlackMentions()\n got  = %q\n want = %q", got, tt.expected)
			}
		})
	}
}
