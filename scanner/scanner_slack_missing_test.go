package scanner

import (
	"message-consolidator/store"
	"testing"
)

func TestGetMinLastTS(t *testing.T) {
	users := []store.User{
		{Email: "user1@example.com"},
		{Email: "user2@example.com"},
	}
	channelID := "C123"

	// Mock store data
	store.UpdateLastScan("user1@example.com", "slack", channelID, "1700000000.000000")
	store.UpdateLastScan("user2@example.com", "slack", channelID, "1700000010.000000")

	got := getMinLastTS(users, channelID)
	want := "1700000000.000000"
	if got != want {
		t.Errorf("getMinLastTS() = %v, want %v", got, want)
	}

	// Test with one empty cursor
	store.UpdateLastScan("user1@example.com", "slack", channelID, "")
	got = getMinLastTS(users, channelID)
	want = ""
	if got != want {
		t.Errorf("getMinLastTS() with empty cursor = %v, want %v", got, want)
	}
}

func TestScanSingleSlackChannel_Logic(t *testing.T) {
	// This test focuses on the coordination between getMinLastTS and sc.GetMessages parameters.
	// Since we cannot easily mock channels.SlackClient without an interface, 
	// we verify the internal logic that feeds into it.
	
	users := []store.User{{Email: "test@example.com"}}
	// c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C999"}}}
	
	// Case: minTS is empty -> should lead to default scan (24h back)
	store.UpdateLastScan("test@example.com", "slack", "C999", "")
	minTS := getMinLastTS(users, "C999")
	if minTS != "" {
		t.Errorf("Expected empty minTS, got %s", minTS)
	}
	
	// Case: minTS is set -> should feed into GetMessages as Oldest
	store.UpdateLastScan("test@example.com", "slack", "C999", "1700000000.000000")
	minTS = getMinLastTS(users, "C999")
	if minTS != "1700000000.000000" {
		t.Errorf("Expected 1700000000.000000, got %s", minTS)
	}
}
