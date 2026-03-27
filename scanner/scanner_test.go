package scanner

import (
	"message-consolidator/store"
	"testing"
)

func TestGetEffectiveAliases(t *testing.T) {
	user := store.User{
		Email: "jjsong@whatap.io",
		Name:  "Jaejin Song",
	}
	aliases := []string{"JJ", "송재진"}

	effective := getEffectiveAliases(user, aliases)

	// 예상되는 배열: ["JJ", "송재진", "Jaejin Song", "jjsong"]
	if len(effective) != 4 {
		t.Errorf("Expected 4 effective aliases, got %d", len(effective))
	}

	expectedMap := map[string]bool{"JJ": true, "송재진": true, "Jaejin Song": true, "jjsong": true}
	for _, a := range effective {
		if !expectedMap[a] {
			t.Errorf("Unexpected alias found: %s", a)
		}
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
		{"Sender Exact Match", "Hello", "JJ", "JJ", true},
		{"Sender Partial Match", "Hello", "Jaejin Song (JJ)", "JJ", true},
		{"Text Exact Match (Long)", "이건 송재진 님이 처리해주세요.", "System", "송재진", true},
		{"Text Partial Match (Short - Korean prefix)", "나는 이 일을 할게요.", "System", "나", true},
		{"Text Exact Match (Short - Korean)", "나 이거 할게", "System", "나", true},
		{"False Positive (Short in Middle)", "지나가다가 봤습니다.", "System", "나", false}, // "지나" 안에 "나"가 있지만 띄어쓰기 규칙으로 무시되어야 함
		{"False Positive (Different Sender)", "Please review", "John", "JJ", false},
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

// mockSlackResolver는 resolveSlackMentions 테스트를 위한 가짜(Mock) 객체입니다.
type mockSlackResolver struct {
	users map[string]string
}

func (m mockSlackResolver) GetUserName(userID string) string {
	return m.users[userID] // 존재하지 않으면 "" 반환
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
			name:     "단일 멘션 치환",
			text:     "Hi <@U0208BU06JE>, for .net agent installation in linux, is it possible?",
			expected: "Hi @Jaejin Song, for .net agent installation in linux, is it possible?",
		},
		{
			name:     "다중 멘션 치환",
			text:     "<@U0208BU06JE> and <@U12345678> are assigned to this task.",
			expected: "@Jaejin Song and @John Doe are assigned to this task.",
		},
		{
			name:     "알 수 없는 사용자 멘션 (치환 없이 원본 유지)",
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
