package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"message-consolidator/types"
)

type fakeReviewAnalyzer struct {
	items  []store.TodoItem
	err    error
	calls  int
	gotRaw string
}

func (f *fakeReviewAnalyzer) Analyze(_ context.Context, _ string, msg types.EnrichedMessage, _, _, _ string) ([]store.TodoItem, error) {
	f.calls++
	f.gotRaw = msg.RawContent
	return f.items, f.err
}

type routedItem struct {
	email string
	item  store.TodoItem
	msg   store.ConsolidatedMessage
}

func newReviewSvc(a ReviewAnalyzer, routed *[]routedItem) *FilterReviewService {
	svc := NewFilterReviewService(a)
	svc.route = func(_ context.Context, _ store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
		*routed = append(*routed, routedItem{email, item, msg})
		return 1, nil
	}
	return svc
}

func seedDrop(t *testing.T, email, verdict string, p float64) int64 {
	t.Helper()
	id, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: store.SourceSlack, Room: "room-a", SourceTS: "1700.1",
		Provider: "clef", PActionable: p, Threshold: 0.3, Verdict: verdict, Text: "please send the report by friday",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return id
}

func setupReviewDB(t *testing.T) func() {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return cleanup
}

func outcomeOf(t *testing.T, email string, id int64) string {
	t.Helper()
	row, err := store.GetFilterScore(context.Background(), email, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return row.Outcome
}

func TestFilterReview_RescueExtractsAndIsIdempotent(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("rescue")
	id := seedDrop(t, email, store.FilterVerdictNoise, 0.2)
	an := &fakeReviewAnalyzer{items: []store.TodoItem{{State: "new", Task: "send report"}, {State: "new", Task: "second"}}}
	var routed []routedItem
	svc := newReviewSvc(an, &routed)

	res, err := svc.Rescue(context.Background(), email, id)
	if err != nil || res.AlreadyRescued || res.Created != 2 {
		t.Fatalf("first rescue: res=%+v err=%v", res, err)
	}
	if an.gotRaw != "please send the report by friday" {
		t.Errorf("analyzer got %q", an.gotRaw)
	}
	if routed[0].msg.SourceTS != "1700.1" || routed[0].msg.Room != "room-a" || routed[0].msg.UserEmail != email {
		t.Errorf("envelope = %+v", routed[0].msg)
	}
	if got := outcomeOf(t, email, id); got != store.FilterOutcomeRescued {
		t.Errorf("outcome = %q", got)
	}

	res, err = svc.Rescue(context.Background(), email, id)
	if err != nil || !res.AlreadyRescued || an.calls != 1 || len(routed) != 2 {
		t.Errorf("second rescue not a no-op: res=%+v err=%v calls=%d routed=%d", res, err, an.calls, len(routed))
	}
}

func TestFilterReview_RescueTenantIsolation(t *testing.T) {
	defer setupReviewDB(t)()
	owner := testutil.RandomEmail("owner")
	other := testutil.RandomEmail("other")
	id := seedDrop(t, owner, store.FilterVerdictNoise, 0.2)
	an := &fakeReviewAnalyzer{items: []store.TodoItem{{State: "new", Task: "x"}}}
	var routed []routedItem
	svc := newReviewSvc(an, &routed)

	if _, err := svc.Rescue(context.Background(), other, id); !errors.Is(err, ErrFilterDropNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if err := svc.Confirm(context.Background(), other, id); !errors.Is(err, ErrFilterDropNotFound) {
		t.Fatalf("confirm err = %v, want not found", err)
	}
	if an.calls != 0 || outcomeOf(t, owner, id) != "" {
		t.Errorf("other user's call must not act: calls=%d", an.calls)
	}
}

func TestFilterReview_RescueRejectsPassRow(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("pass")
	id := seedDrop(t, email, store.FilterVerdictPass, 0.9)
	var routed []routedItem
	svc := newReviewSvc(&fakeReviewAnalyzer{}, &routed)
	if _, err := svc.Rescue(context.Background(), email, id); !errors.Is(err, ErrFilterNotADrop) {
		t.Fatalf("err = %v", err)
	}
}

func TestFilterReview_AnalyzeFailureKeepsDropRetryable(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("fail")
	id := seedDrop(t, email, store.FilterVerdictNoise, 0.2)
	var routed []routedItem
	svc := newReviewSvc(&fakeReviewAnalyzer{err: errors.New("boom")}, &routed)
	if _, err := svc.Rescue(context.Background(), email, id); err == nil {
		t.Fatal("want error")
	}
	if got := outcomeOf(t, email, id); got != "" {
		t.Errorf("outcome = %q, want empty so the user can retry", got)
	}
}

func TestFilterReview_RescueWithoutAnalyzer(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("noai")
	id := seedDrop(t, email, store.FilterVerdictNoise, 0.2)
	svc := NewFilterReviewService(nil)
	if _, err := svc.Rescue(context.Background(), email, id); !errors.Is(err, ErrRescueUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestFilterReview_ConfirmDoesNotOverrideRescue(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("confirm")
	id := seedDrop(t, email, store.FilterVerdictNoise, 0.2)
	var routed []routedItem
	svc := newReviewSvc(&fakeReviewAnalyzer{}, &routed)

	if err := svc.Confirm(context.Background(), email, id); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, email, id); got != store.FilterOutcomeConfirmedNoise {
		t.Fatalf("outcome = %q", got)
	}
	// Why: a user may reverse a confirmation; the rescue must win.
	if _, err := svc.Rescue(context.Background(), email, id); err != nil {
		t.Fatal(err)
	}
	if err := svc.Confirm(context.Background(), email, id); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, email, id); got != store.FilterOutcomeRescued {
		t.Errorf("outcome = %q, want rescued kept", got)
	}
}

func TestFilterReview_ListDropsHeadAndScope(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("list")
	seedDrop(t, email, store.FilterVerdictPass, 0.9)
	long := make([]rune, 500)
	for i := range long {
		long[i] = '가'
	}
	if _, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: "slack", Provider: "clef", Verdict: store.FilterVerdictNoise, Text: string(long),
	}); err != nil {
		t.Fatal(err)
	}
	seedDrop(t, testutil.RandomEmail("elsewhere"), store.FilterVerdictNoise, 0.1)

	svc := NewFilterReviewService(nil)
	drops, err := svc.ListDrops(context.Background(), email, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(drops) != 1 {
		t.Fatalf("len = %d, want 1 (noise rows of this user only)", len(drops))
	}
	if n := len([]rune(drops[0].Text)); n != FilterDropTextHeadRunes {
		t.Errorf("text runes = %d, want %d", n, FilterDropTextHeadRunes)
	}
}

func TestFilterReview_RescueGmailUsesGmailRoom(t *testing.T) {
	defer setupReviewDB(t)()
	email := testutil.RandomEmail("rescue-gmail")
	id, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: store.SourceGmail, SourceTS: "gmail-abc", MessageRef: "abc",
		Provider: "clef", PActionable: 0.2, Threshold: 0.3, Verdict: store.FilterVerdictNoise, Text: "please review the contract",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	var routed []routedItem
	svc := newReviewSvc(&fakeReviewAnalyzer{items: []store.TodoItem{{State: "new", Task: "review contract"}}}, &routed)

	if _, err := svc.Rescue(context.Background(), email, id); err != nil {
		t.Fatalf("rescue: %v", err)
	}
	if len(routed) != 1 || routed[0].msg.Room != "Gmail" || routed[0].msg.SourceTS != "gmail-abc" {
		t.Errorf("routed = %+v, want one item in room Gmail with source_ts gmail-abc", routed)
	}
}
