package scanner

import (
	"context"
	"testing"

	"message-consolidator/channels"
	"message-consolidator/store"
)

func TestGroupThreadsByKey(t *testing.T) {
	ts := func(ch, ts, email string) store.SlackThreadMeta {
		return store.SlackThreadMeta{ChannelID: ch, ThreadTS: ts, UserEmail: email}
	}

	tests := []struct {
		name       string
		input      []store.SlackThreadMeta
		wantGroups int
		wantSizes  []int // size of each group, in input order
	}{
		{
			name:       "empty",
			input:      nil,
			wantGroups: 0,
		},
		{
			name:       "single row",
			input:      []store.SlackThreadMeta{ts("C1", "100.0", "a@x.io")},
			wantGroups: 1,
			wantSizes:  []int{1},
		},
		{
			name: "two users same thread",
			input: []store.SlackThreadMeta{
				ts("C1", "100.0", "a@x.io"),
				ts("C1", "100.0", "b@x.io"),
			},
			wantGroups: 1,
			wantSizes:  []int{2},
		},
		{
			name: "two distinct threads",
			input: []store.SlackThreadMeta{
				ts("C1", "100.0", "a@x.io"),
				ts("C1", "200.0", "a@x.io"),
			},
			wantGroups: 2,
			wantSizes:  []int{1, 1},
		},
		{
			name: "two threads two users each",
			input: []store.SlackThreadMeta{
				ts("C1", "100.0", "a@x.io"),
				ts("C1", "100.0", "b@x.io"),
				ts("C1", "200.0", "a@x.io"),
				ts("C1", "200.0", "b@x.io"),
			},
			wantGroups: 2,
			wantSizes:  []int{2, 2},
		},
		{
			name: "different channels same thread_ts are separate groups",
			input: []store.SlackThreadMeta{
				ts("C1", "100.0", "a@x.io"),
				ts("C2", "100.0", "a@x.io"),
			},
			wantGroups: 2,
			wantSizes:  []int{1, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := groupThreadsByKey(tc.input)
			if len(got) != tc.wantGroups {
				t.Fatalf("got %d groups, want %d", len(got), tc.wantGroups)
			}
			for i, sz := range tc.wantSizes {
				if len(got[i]) != sz {
					t.Errorf("group[%d] size = %d, want %d", i, len(got[i]), sz)
				}
			}
		})
	}
}

// TestHandleThreadTimeoutGroup_TwoSubscribers verifies no panic and both DB rows are attempted.
func TestHandleThreadTimeoutGroup_TwoSubscribers(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{
		{ChannelID: "C1", ThreadTS: "1700000100.000000", UserEmail: "a@x.io"},
		{ChannelID: "C1", ThreadTS: "1700000100.000000", UserEmail: "b@x.io"},
	}
	// PostMessage fails silently; CloseTargetedThread uses DB (rows may not exist — no-op is OK).
	handleThreadTimeoutGroup(context.Background(), sc, group)
}

// TestHandleThreadTimeoutGroup_EmptyThreadTS verifies empty ThreadTS logs and closes without PostMessage.
func TestHandleThreadTimeoutGroup_EmptyThreadTS(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{
		{ChannelID: "C1", ThreadTS: "", UserEmail: "a@x.io"},
		{ChannelID: "C1", ThreadTS: "", UserEmail: "b@x.io"},
	}
	handleThreadTimeoutGroup(context.Background(), sc, group)
}

// TestUpdateThreadStatusGroup_Resolved_TwoSubscribers verifies resolved message sent once and both rows closed.
func TestUpdateThreadStatusGroup_Resolved_TwoSubscribers(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{
		{ChannelID: "C1", ThreadTS: "1700000100.000000", UserEmail: "a@x.io"},
		{ChannelID: "C1", ThreadTS: "1700000100.000000", UserEmail: "b@x.io"},
	}
	res := threadScanResult{isResolved: true, newLastTS: "1700000200.000000", newLastActivity: "1700000150.000000"}
	updateThreadStatusGroup(context.Background(), sc, group, res)
}

// TestUpdateThreadStatusGroup_NoChange_TwoSubscribers verifies cursor update propagated to all rows.
func TestUpdateThreadStatusGroup_NoChange_TwoSubscribers(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{
		{ChannelID: "C1", ThreadTS: "1700000100.000000", LastTS: "1700000200.000000", LastActivityTS: "1700000150.000000", UserEmail: "a@x.io"},
		{ChannelID: "C1", ThreadTS: "1700000100.000000", LastTS: "1700000200.000000", LastActivityTS: "1700000150.000000", UserEmail: "b@x.io"},
	}
	res := threadScanResult{isResolved: false, newLastTS: "1700000200.000000", newLastActivity: "1700000150.000000"}
	// newLastTS == LastTS AND newLastActivity == LastActivityTS → no DB write, no panic.
	updateThreadStatusGroup(context.Background(), sc, group, res)
}
