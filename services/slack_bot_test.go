package services

import (
	"message-consolidator/store"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestParseDMCommand(t *testing.T) {
	cases := []struct {
		in   string
		kind string
		arg  string
	}{
		{"tasks", "tasks", ""},
		{"  TASKS  ", "tasks", ""},
		{"task", "tasks", ""},
		{"list", "tasks", ""},
		{"<@U123> tasks", "tasks", ""},
		{"<@U123>   list", "tasks", ""},
		{"done 42", "done", "42"},
		{"DONE 42", "done", "42"},
		{"complete 99", "done", "99"},
		{"done", "done", ""},
		{"help", "help", ""},
		{"?", "help", ""},
		{"", "help", ""},
		{"<@U123>", "help", ""},
		{"unknown stuff", "", ""},
		// grant/revoke
		{"grant yosep", "grant", "yosep"},
		{"GRANT yosep", "grant", "yosep"},
		{"allow yosep", "grant", "yosep"},
		{"revoke yosep", "revoke", "yosep"},
		{"deny yosep", "revoke", "yosep"},
		{"grant", "grant", ""},
		{"revoke", "revoke", ""},
		// tasks with arg
		{"tasks yosep", "tasks", "yosep"},
		{"task yosep", "tasks", "yosep"},
		{"<@U123> tasks yosep", "tasks", "yosep"},
		// multi-word name
		{"tasks 홍 길동", "tasks", "홍 길동"},
		{"grant 홍 길동", "grant", "홍 길동"},
	}
	for _, tc := range cases {
		got := ParseDMCommand(tc.in)
		if got.Kind != tc.kind || got.Arg != tc.arg {
			t.Errorf("ParseDMCommand(%q) = (%q,%q), want (%q,%q)", tc.in, got.Kind, got.Arg, tc.kind, tc.arg)
		}
	}
}

func TestParseSlackActionID(t *testing.T) {
	cases := []struct {
		in        string
		kind, arg string
		ok        bool
	}{
		{"task_done:123", "task_done", "123", true},
		{"task_page:2", "task_page", "2", true},
		{"task_done:", "", "", false},
		{":123", "", "", false},
		{"no_colon", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		k, a, ok := ParseSlackActionID(tc.in)
		if k != tc.kind || a != tc.arg || ok != tc.ok {
			t.Errorf("ParseSlackActionID(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.in, k, a, ok, tc.kind, tc.arg, tc.ok)
		}
	}
}

func TestBuildTaskListBlocks_Empty(t *testing.T) {
	blocks, fb := BuildTaskListBlocks(nil, 0, SlackBotPageSize, 0, "")
	if len(blocks) != 1 {
		t.Fatalf("empty list should render single section, got %d blocks", len(blocks))
	}
	if fb == "" {
		t.Errorf("fallback must be non-empty for accessibility")
	}
}

func TestBuildTaskListBlocks_PageButtonVisibility(t *testing.T) {
	tasks := []store.ConsolidatedMessage{{ID: 1, Task: "a"}, {ID: 2, Task: "b"}}

	blocks, _ := BuildTaskListBlocks(tasks, 0, 10, 2, "")
	if hasPaginationActionBlock(blocks) {
		t.Errorf("page button must not appear when total <= pageSize")
	}

	blocks2, _ := BuildTaskListBlocks(tasks, 0, 2, 5, "")
	if !hasPaginationActionBlock(blocks2) {
		t.Errorf("page button must appear when total > (page+1)*pageSize")
	}
}

func TestBuildTaskListBlocks_BlockCount(t *testing.T) {
	tasks := []store.ConsolidatedMessage{
		{ID: 11, Task: "first"},
		{ID: 22, Task: "second"},
	}
	blocks, _ := BuildTaskListBlocks(tasks, 0, 10, 2, "")
	// header + divider + 2 task sections = 4 blocks (no pagination)
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks (header+divider+2 tasks), got %d", len(blocks))
	}
}

func TestBuildTaskListBlocks_OwnerName(t *testing.T) {
	task := []store.ConsolidatedMessage{{ID: 1, Task: "do something"}}

	// own view: accessory (done button) must exist
	blocksOwn, fbOwn := BuildTaskListBlocks(task, 0, 10, 1, "")
	if !blockHasAccessory(blocksOwn) {
		t.Errorf("own view: task section must have an accessory (done button)")
	}
	if strings.Contains(fbOwn, "의 활성 task") {
		t.Errorf("own view fallback must not contain '의 활성 task', got %q", fbOwn)
	}

	// delegated view: no accessory (read-only)
	blocksOther, fbOther := BuildTaskListBlocks(task, 0, 10, 1, "yosep")
	if blockHasAccessory(blocksOther) {
		t.Errorf("delegated view: task section must NOT have an accessory (read-only)")
	}
	if !strings.Contains(fbOther, "yosep의 활성 task") {
		t.Errorf("delegated view fallback must contain 'yosep의 활성 task', got %q", fbOther)
	}
}

// TestResolveUserByName_Logic documents expected matching behavior.
// Integration coverage is in store/grant_store_test.go.
// The actual dispatch integration (HandleDMText → resolveUserByName → IsGrantedToView)
// requires a real DB and is omitted from the unit test suite.
func TestResolveUserByName_Logic(t *testing.T) {
	t.Skip("resolveUserByName requires store.GetAllUsers (DB); see store/grant_store_test.go")
}

func hasPaginationActionBlock(blocks []slack.Block) bool {
	for _, b := range blocks {
		if ab, ok := b.(*slack.ActionBlock); ok && ab.BlockID == "task_pagination" {
			return true
		}
	}
	return false
}

func blockHasAccessory(blocks []slack.Block) bool {
	for _, b := range blocks {
		sb, ok := b.(*slack.SectionBlock)
		if !ok {
			continue
		}
		if sb.Accessory != nil {
			return true
		}
	}
	return false
}

