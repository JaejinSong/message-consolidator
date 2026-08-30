package services

import (
	"context"
	"encoding/json"
	"testing"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func setupCorrectionLearningTestDB(t *testing.T) func() {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	return cleanup
}

// seedTaskRow inserts a message row and returns its ID, for tests that need a real
// message_id to satisfy the seen_message_ids / learned_examples FK-like columns.
func seedTaskRow(t *testing.T, email, task string, done bool) store.MessageID {
	t.Helper()
	src := testutil.RandomTS("corr")
	doneInt := 0
	if done {
		doneInt = 1
	}
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, category, source, room, source_ts, original_text, done, is_deleted)
		 VALUES (?, ?, 'TASK', 'whatsapp', 'general', ?, ?, ?, 0)`,
		email, task, src, task+" original text", doneInt,
	)
	if err != nil {
		t.Fatalf("seedTaskRow: %v", err)
	}
	id, _ := res.LastInsertId()
	return store.MessageID(id)
}

func getObservation(t *testing.T, email, kind, from, to, scope string) (db.CorrectionObservation, bool) {
	t.Helper()
	obs, err := db.New(store.GetDB()).GetCorrectionObservation(context.Background(), db.GetCorrectionObservationParams{
		UserEmail: email, Kind: kind, FromValue: from, ToValue: to, Scope: scope,
	})
	if err != nil {
		return db.CorrectionObservation{}, false
	}
	return obs, true
}

func TestUpsertCorrectionObservation_DistinctIDsPromote(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("upsert-distinct")

	justPromoted, err := upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 1, alignPromoteThreshold)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if justPromoted {
		t.Error("first evidence must not promote")
	}
	obs, ok := getObservation(t, email, "assignee_alias", "bob", "robert", "general")
	if !ok || obs.EvidenceCount != 1 || obs.Status != "pending" {
		t.Fatalf("after first upsert: obs=%+v ok=%v", obs, ok)
	}

	justPromoted, err = upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 2, alignPromoteThreshold)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if !justPromoted {
		t.Error("second distinct id must cross alignPromoteThreshold and promote")
	}
	obs, ok = getObservation(t, email, "assignee_alias", "bob", "robert", "general")
	if !ok || obs.EvidenceCount != 2 || obs.Status != "promoted" {
		t.Fatalf("after second upsert: obs=%+v ok=%v", obs, ok)
	}
}

func TestUpsertCorrectionObservation_SameIDTwiceNoDoubleCount(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("upsert-sameid")

	if _, err := upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 1, alignPromoteThreshold); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	justPromoted, err := upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 1, alignPromoteThreshold)
	if err != nil {
		t.Fatalf("repeat upsert: %v", err)
	}
	if justPromoted {
		t.Error("a repeated message id must not add new evidence or promote")
	}
	obs, ok := getObservation(t, email, "assignee_alias", "bob", "robert", "general")
	if !ok || obs.EvidenceCount != 1 {
		t.Fatalf("evidence must stay at 1 for a repeated id, got %+v", obs)
	}
}

func TestUpsertCorrectionObservation_SuppressNeedsThreeDistinctIDs(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("upsert-suppress")

	var lastPromoted bool
	var err error
	for i := int64(1); i <= 3; i++ {
		lastPromoted, err = upsertCorrectionObservation(context.Background(), email, "suppress", "spam tokens", "", "whatsapp|general", store.MessageID(i), suppressPromoteThreshold)
		if err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
		if i < 3 && lastPromoted {
			t.Errorf("must not promote before evidence=%d reaches suppressPromoteThreshold=%d", i, suppressPromoteThreshold)
		}
	}
	if !lastPromoted {
		t.Error("third distinct id must promote a suppress observation")
	}
}

func TestUpsertCorrectionObservation_RejectedNeverResurrects(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("upsert-rejected")

	if _, err := upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 1, alignPromoteThreshold); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := DecideObservation(context.Background(), email, mustObservationID(t, email, "assignee_alias", "bob", "robert", "general"), false); err != nil {
		t.Fatalf("reject: %v", err)
	}

	justPromoted, err := upsertCorrectionObservation(context.Background(), email, "assignee_alias", "bob", "robert", "general", 2, alignPromoteThreshold)
	if err != nil {
		t.Fatalf("upsert after reject: %v", err)
	}
	if justPromoted {
		t.Error("a rejected observation must never promote again")
	}
	obs, ok := getObservation(t, email, "assignee_alias", "bob", "robert", "general")
	if !ok || obs.Status != "rejected" || obs.EvidenceCount != 1 {
		t.Fatalf("rejected observation must stay rejected with unchanged evidence, got %+v", obs)
	}
}

func mustObservationID(t *testing.T, email, kind, from, to, scope string) int64 {
	t.Helper()
	obs, ok := getObservation(t, email, kind, from, to, scope)
	if !ok {
		t.Fatalf("observation not found: %s %s->%s scope=%s", kind, from, to, scope)
	}
	return obs.ID
}

func TestDecideObservation_ApproveSetsStatus(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("decide-approve")

	if _, err := upsertCorrectionObservation(context.Background(), email, "deadline_expr", "eow", "friday", "slack", 1, alignPromoteThreshold); err != nil {
		t.Fatalf("seed: %v", err)
	}
	id := mustObservationID(t, email, "deadline_expr", "eow", "friday", "slack")
	if err := DecideObservation(context.Background(), email, id, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	obs, ok := getObservation(t, email, "deadline_expr", "eow", "friday", "slack")
	if !ok || obs.Status != "approved" {
		t.Fatalf("expected approved status, got %+v", obs)
	}
}

func TestRecordSingleDeletion_DoneTaskIgnored(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("delete-done")
	id := seedTaskRow(t, email, "Completed task", true)

	msg := store.ConsolidatedMessage{ID: id, UserEmail: email, Source: "whatsapp", Room: "general", Done: true, OriginalText: "some real conversation text here"}
	recordSingleDeletion(context.Background(), email, msg)

	var count int
	if err := store.GetDB().QueryRow(`SELECT COUNT(*) FROM correction_observations WHERE user_email = ?`, email).Scan(&count); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if count != 0 {
		t.Errorf("a done task's deletion must not create any suppress observation, got %d rows", count)
	}
}

func TestRecordSingleDeletion_NegativeExampleOnlyOnPromotion(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("delete-negex")
	originalText := "irrelevant spam broadcast message"

	// First deletion: evidence=1, must not promote and must not add a negative example.
	id1 := seedTaskRow(t, email, "Spam task", false)
	msg1 := store.ConsolidatedMessage{ID: id1, UserEmail: email, Source: "whatsapp", Room: "general", OriginalText: originalText}
	recordSingleDeletion(context.Background(), email, msg1)

	count, err := db.New(store.GetDB()).CountLearnedExamplesByOrigin(context.Background(), db.CountLearnedExamplesByOriginParams{UserEmail: email, Origin: "delete_negative"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no delete_negative example before promotion, got %d", count)
	}

	// Second and third distinct-id deletions cross suppressPromoteThreshold=3.
	id2 := seedTaskRow(t, email, "Spam task 2", false)
	recordSingleDeletion(context.Background(), email, store.ConsolidatedMessage{ID: id2, UserEmail: email, Source: "whatsapp", Room: "general", OriginalText: originalText})
	id3 := seedTaskRow(t, email, "Spam task 3", false)
	recordSingleDeletion(context.Background(), email, store.ConsolidatedMessage{ID: id3, UserEmail: email, Source: "whatsapp", Room: "general", OriginalText: originalText})

	count, err = db.New(store.GetDB()).CountLearnedExamplesByOrigin(context.Background(), db.CountLearnedExamplesByOriginParams{UserEmail: email, Origin: "delete_negative"})
	if err != nil {
		t.Fatalf("count after promotion: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one delete_negative example on promotion, got %d", count)
	}
}

func TestMarkFieldSources_MergesIntoExisting(t *testing.T) {
	base, err := MetadataSet(nil, "field_sources", map[string]string{"deadline": "manual"})
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	updated, err := MarkFieldSources(base, []string{"assignee"})
	if err != nil {
		t.Fatalf("MarkFieldSources: %v", err)
	}
	var sources map[string]string
	if _, err := MetadataGet(updated, "field_sources", &sources); err != nil {
		t.Fatalf("read back field_sources: %v", err)
	}
	if sources["deadline"] != "manual" || sources["assignee"] != "manual" {
		t.Errorf("expected both fields marked manual, got %+v", sources)
	}
}

func TestMarkFieldSources_NoEditsNoop(t *testing.T) {
	updated, err := MarkFieldSources(nil, nil)
	if err != nil {
		t.Fatalf("MarkFieldSources: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("expected metadata to stay untouched, got %q", updated)
	}
}

func TestRecordTaskEditWork_MinesObservationsAndStoresExample(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("edit-mine")
	id := seedTaskRow(t, email, "Original task", false)

	original := map[string]string{"task": "Original task", "assignee": "shared", "deadline": "", "category": "TASK"}
	meta, err := MetadataSet(nil, "ai_original", original)
	if err != nil {
		t.Fatalf("seed ai_original: %v", err)
	}

	before := store.ConsolidatedMessage{
		ID: id, UserEmail: email, Source: "whatsapp", Room: "general",
		Task: "Original task", Assignee: "shared", Requester: "alice",
		OriginalText: "please handle this for me", Metadata: meta,
	}
	newAssignee := "bob"
	recordTaskEditWork(context.Background(), email, before, EditFields{Assignee: &newAssignee})

	if _, ok := getObservation(t, email, "assignee_alias", "shared", "bob", "general"); !ok {
		t.Error("expected an assignee_alias observation to be recorded")
	}

	examples, err := db.New(store.GetDB()).ListLearnedExamples(context.Background(), db.ListLearnedExamplesParams{UserEmail: email, Limit: 10})
	if err != nil {
		t.Fatalf("list examples: %v", err)
	}
	if len(examples) != 1 || examples[0].Origin != "edit_confirm" {
		t.Fatalf("expected one edit_confirm example, got %+v", examples)
	}
	var decoded []learnedExpectedItem
	if err := json.Unmarshal([]byte(examples[0].Expected), &decoded); err != nil {
		t.Fatalf("decode expected: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Assignee != "bob" {
		t.Fatalf("expected example to reflect the post-edit assignee, got %+v", decoded)
	}
}

func TestFieldIsManual(t *testing.T) {
	meta, err := MetadataSet(nil, "field_sources", map[string]string{"assignee": "manual"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !fieldIsManual(meta, "assignee") {
		t.Error("expected assignee to be reported manual")
	}
	if fieldIsManual(meta, "task") {
		t.Error("task was not marked manual")
	}
	if fieldIsManual(nil, "assignee") {
		t.Error("nil metadata must report false")
	}
}
