package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func seedPendingCandidate(t *testing.T, email, task, text, detectedAt string) {
	t.Helper()
	meta := `{"completion_candidate":{"source_text":"` + text + `","detected_at":"` + detectedAt + `","status":"pending"}}`
	_, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata, requester, assignee)
		 VALUES (?, ?, 'TASK', 'slack', 'room-a', ?, 0, 0, ?, 'req', 'asg')`,
		email, task, testutil.RandomTS("cand"), meta,
	)
	if err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
}

func TestCandidateDigestBlocks_Empty(t *testing.T) {
	defer setupReviewDB(t)()
	s := &DailyDigestService{nowFn: time.Now}
	if blocks := s.buildCandidateBlocks(context.Background(), testutil.RandomEmail("cand")); blocks != nil {
		t.Errorf("want nil, got %d blocks", len(blocks))
	}
}

func TestCandidateDigestBlocks_RendersEscapedRowsOldestFirst(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("cand")
	seedPendingCandidate(t, email, "newer <task>", "later", "2026-10-02T00:00:00Z")
	seedPendingCandidate(t, email, "older & task", "report was sent <ok>", "2026-10-01T00:00:00Z")

	s := &DailyDigestService{nowFn: time.Now, Config: DailyDigestConfig{AppBaseURL: "https://app.example/"}}
	text := blocksText(s.buildCandidateBlocks(context.Background(), email))

	if !strings.Contains(text, "*✅ 완료로 보이는 태스크 (확인 필요)*") {
		t.Errorf("missing header: %s", text)
	}
	if !strings.Contains(text, `• older &amp; task  (slack/room-a) — "report was sent &lt;ok&gt;"`) {
		t.Errorf("row not rendered/escaped: %s", text)
	}
	if strings.Index(text, "older") > strings.Index(text, "newer") {
		t.Errorf("not oldest first: %s", text)
	}
	if !strings.Contains(text, "<https://app.example|앱에서 확인하기>") {
		t.Errorf("missing link: %s", text)
	}
}

func TestCandidateDigestBlocks_NoItalicsAndSingleLine(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("cand")
	_, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, done, is_deleted, metadata, requester, assignee)
		 VALUES (?, 'a *bold* task', 'TASK', 'slack', 'team_room_x', ?, 0, 0, ?, 'req', 'asg')`,
		email, testutil.RandomTS("cand"),
		`{"completion_candidate":{"source_text":"line one\nline _two_","detected_at":"2026-10-01T00:00:00Z","status":"pending"}}`,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := &DailyDigestService{nowFn: time.Now, Config: DailyDigestConfig{AppBaseURL: "https://app.example"}}
	blocks := s.buildCandidateBlocks(context.Background(), email)
	row := blocks[2].(*slack.SectionBlock).Text.Text
	want := `• a  bold  task  (slack/team room x) — "line one line  two "`
	if row != want {
		t.Errorf("row = %q, want %q", row, want)
	}
}
