package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"message-consolidator/db"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"

	"github.com/whatap/go-api/trace"
)

const (
	suppressPromoteThreshold = 3  // Why: false suppression silently hides tasks; demand more distinct evidence.
	alignPromoteThreshold    = 2  // Why: over-extraction is visible and cheap to delete; promote aggressively.
	completionExampleCap     = 29 // Why: bound the weak-positive pool; prime per project convention.
)

// EditFields carries the optional per-field edits a user made to a task. A nil
// pointer means the field was untouched by this edit.
type EditFields struct {
	Task     *string
	Assignee *string
	Deadline *string
	Category *string
}

// learnedExpectedItem mirrors the AI extraction JSON shape (see ai/core/few_shots.go)
// so learned examples can be replayed as few-shots without reshaping.
type learnedExpectedItem struct {
	Task      string `json:"task"`
	Requester string `json:"requester,omitempty"`
	Assignee  string `json:"assignee,omitempty"`
	Deadline  string `json:"deadline,omitempty"`
	Category  string `json:"category,omitempty"`
	SourceTS  string `json:"source_ts,omitempty"`
}

// MarkFieldSources records which fields on a task were set by a human edit.
// Why: principle 6 -- a human decision must never be silently overwritten by a
// later AI rescan; task_routing.go's update path consults this marker.
func MarkFieldSources(meta json.RawMessage, edited []string) (json.RawMessage, error) {
	if len(edited) == 0 {
		return meta, nil
	}
	existing := map[string]string{}
	// Why: malformed/missing existing sources must not block marking new ones.
	_, _ = MetadataGet(meta, "field_sources", &existing)
	for _, f := range edited {
		existing[f] = "manual"
	}
	return MetadataSet(meta, "field_sources", existing)
}

// fieldIsManual reports whether field was marked as a human edit via MarkFieldSources.
// Why: rescan overwrite protection -- an AI-driven update must never silently
// overwrite a value the user explicitly set (principle 6).
func fieldIsManual(meta json.RawMessage, field string) bool {
	sources := map[string]string{}
	if _, err := MetadataGet(meta, "field_sources", &sources); err != nil {
		return false
	}
	return sources[field] == "manual"
}

// RecordTaskEdit mines a confirmed user edit for correction-learning signal.
// Why: fire-and-forget -- a learning failure must never block or fail the user's save.
func RecordTaskEdit(ctx context.Context, userEmail string, before store.ConsolidatedMessage, after EditFields) {
	dctx := context.WithoutCancel(ctx)
	go func() {
		defer safego.Recover("correction-learning-edit")
		traceCtx, _ := trace.Start(dctx, "/CorrectionLearning-Edit")
		defer func() { _ = trace.End(traceCtx, nil) }()
		recordTaskEditWork(traceCtx, userEmail, before, after)
	}()
}

func recordTaskEditWork(ctx context.Context, userEmail string, before store.ConsolidatedMessage, after EditFields) {
	var original map[string]string
	hasOriginal, err := MetadataGet(before.Metadata, "ai_original", &original)
	if err != nil {
		logger.Warnf("[LEARNING] read ai_original for msg %d: %v", before.ID, err)
	}
	if hasOriginal {
		mineEditObservations(ctx, userEmail, before, after, original)
	}
	storeEditConfirmExample(ctx, userEmail, before, after)
}

// mineEditObservations upserts one correction_observations row per field that both
// changed and differs from the AI's original extraction (1:1 replacement signal).
func mineEditObservations(ctx context.Context, userEmail string, before store.ConsolidatedMessage, after EditFields, original map[string]string) {
	if after.Assignee != nil && *after.Assignee != original["assignee"] {
		upsertAndLog(ctx, userEmail, "assignee_alias", original["assignee"], *after.Assignee, before.Room, before.ID)
	}
	if after.Deadline != nil && *after.Deadline != original["deadline"] {
		upsertAndLog(ctx, userEmail, "deadline_expr", original["deadline"], *after.Deadline, before.Source, before.ID)
	}
	if after.Category != nil && *after.Category != original["category"] {
		upsertAndLog(ctx, userEmail, "category_boundary", original["category"], *after.Category, "", before.ID)
	}
}

func upsertAndLog(ctx context.Context, userEmail, kind, from, to, scope string, msgID store.MessageID) {
	if _, err := upsertCorrectionObservation(ctx, userEmail, kind, from, to, scope, msgID, alignPromoteThreshold); err != nil {
		logger.Warnf("[LEARNING] upsert %s observation: %v", kind, err)
	}
}

// storeEditConfirmExample persists the post-edit values as a strong-positive learned
// example. Skipped when there is no original text to learn from.
func storeEditConfirmExample(ctx context.Context, userEmail string, before store.ConsolidatedMessage, after EditFields) {
	if before.OriginalText == "" {
		return
	}
	item := learnedExpectedItem{
		Task:      pickString(after.Task, before.Task),
		Requester: before.Requester,
		Assignee:  pickString(after.Assignee, before.Assignee),
		Deadline:  pickString(after.Deadline, before.Deadline),
		Category:  pickString(after.Category, before.Category),
		SourceTS:  before.SourceTS,
	}
	expected, err := json.Marshal([]learnedExpectedItem{item})
	if err != nil {
		logger.Warnf("[LEARNING] marshal edit_confirm expected for msg %d: %v", before.ID, err)
		return
	}
	insertLearnedExample(ctx, userEmail, before.Source, before.OriginalText, string(expected), "edit_confirm", before.ID)
}

func pickString(edited *string, original string) string {
	if edited != nil {
		return *edited
	}
	return original
}

// RecordTaskDeletion mines deleted tasks for false-positive suppression signal.
// Why: fire-and-forget -- a learning failure must never block or fail the user's delete.
func RecordTaskDeletion(ctx context.Context, userEmail string, msgs []store.ConsolidatedMessage) {
	dctx := context.WithoutCancel(ctx)
	go func() {
		defer safego.Recover("correction-learning-delete")
		traceCtx, _ := trace.Start(dctx, "/CorrectionLearning-Delete")
		defer func() { _ = trace.End(traceCtx, nil) }()
		for _, m := range msgs {
			recordSingleDeletion(traceCtx, userEmail, m)
		}
	}()
}

func recordSingleDeletion(ctx context.Context, userEmail string, m store.ConsolidatedMessage) {
	// Why: an informal completion (marked done) is not a false positive -- only an
	// undone deletion signals "the AI should not have surfaced this."
	if m.Done || m.OriginalText == "" {
		return
	}
	fromValue := strings.Join(suppressSignature(m.OriginalText), " ")
	if fromValue == "" {
		return
	}
	scope := m.Source + "|" + m.Room
	justPromoted, err := upsertCorrectionObservation(ctx, userEmail, "suppress", fromValue, "", scope, m.ID, suppressPromoteThreshold)
	if err != nil {
		logger.Warnf("[LEARNING] upsert suppress observation: %v", err)
		return
	}
	// Why: one deletion is noise; the negative few-shot is the strong lever, only
	// worth adding once the pattern crosses the promotion threshold.
	if !justPromoted {
		return
	}
	insertLearnedExample(ctx, userEmail, m.Source, m.OriginalText, "[]", "delete_negative", m.ID)
}

// RecordManualAdd mines a user-created task the AI missed -- the highest-value
// signal (false negative), stored immediately with no evidence threshold.
// Why: fire-and-forget -- a learning failure must never block or fail the user's create.
func RecordManualAdd(ctx context.Context, userEmail string, msg store.ConsolidatedMessage, originalText string) {
	if originalText == "" {
		return
	}
	dctx := context.WithoutCancel(ctx)
	go func() {
		defer safego.Recover("correction-learning-manual-add")
		traceCtx, _ := trace.Start(dctx, "/CorrectionLearning-ManualAdd")
		defer func() { _ = trace.End(traceCtx, nil) }()
		recordManualAddWork(traceCtx, userEmail, msg, originalText)
	}()
}

func recordManualAddWork(ctx context.Context, userEmail string, msg store.ConsolidatedMessage, originalText string) {
	item := learnedExpectedItem{
		Task: msg.Task, Requester: msg.Requester, Assignee: msg.Assignee,
		Deadline: msg.Deadline, Category: msg.Category, SourceTS: msg.SourceTS,
	}
	expected, err := json.Marshal([]learnedExpectedItem{item})
	if err != nil {
		logger.Warnf("[LEARNING] marshal manual_add expected for msg %d: %v", msg.ID, err)
		return
	}
	insertLearnedExample(ctx, userEmail, msg.Source, originalText, string(expected), "manual_add", msg.ID)
}

// RecordUneditedCompletion mines a weak-positive learned example from a task the
// user completed without ever editing -- a soft confirmation the AI extraction was
// good enough, capped so it cannot drown out stronger signals.
// Why: fire-and-forget -- a learning failure must never block or fail the user's completion.
func RecordUneditedCompletion(ctx context.Context, userEmail string, msg store.ConsolidatedMessage) {
	dctx := context.WithoutCancel(ctx)
	go func() {
		defer safego.Recover("correction-learning-completion")
		traceCtx, _ := trace.Start(dctx, "/CorrectionLearning-Completion")
		defer func() { _ = trace.End(traceCtx, nil) }()
		recordUneditedCompletionWork(traceCtx, userEmail, msg)
	}()
}

func recordUneditedCompletionWork(ctx context.Context, userEmail string, msg store.ConsolidatedMessage) {
	if msg.OriginalText == "" {
		return
	}
	var sources map[string]string
	if hasSources, _ := MetadataGet(msg.Metadata, "field_sources", &sources); hasSources && len(sources) > 0 {
		return // Why: user already edited this task -- the edit path already recorded it.
	}
	var original map[string]string
	if hasOriginal, _ := MetadataGet(msg.Metadata, "ai_original", &original); !hasOriginal {
		return
	}
	count, err := db.New(store.GetDB()).CountLearnedExamplesByOrigin(ctx, db.CountLearnedExamplesByOriginParams{
		UserEmail: userEmail, Origin: "completion",
	})
	if err != nil {
		logger.Warnf("[LEARNING] count completion examples for %s: %v", userEmail, err)
		return
	}
	if count >= completionExampleCap {
		return
	}
	item := learnedExpectedItem{
		Task: msg.Task, Requester: msg.Requester, Assignee: msg.Assignee,
		Deadline: msg.Deadline, Category: msg.Category, SourceTS: msg.SourceTS,
	}
	expected, err := json.Marshal([]learnedExpectedItem{item})
	if err != nil {
		logger.Warnf("[LEARNING] marshal completion expected for msg %d: %v", msg.ID, err)
		return
	}
	insertLearnedExample(ctx, userEmail, msg.Source, msg.OriginalText, string(expected), "completion", msg.ID)
}

// DecideObservation applies a human decision to a pending correction observation.
// Rejection is permanent -- upsertCorrectionObservation refuses to add evidence to a
// rejected row, so it can never resurrect. Ownership is enforced by the user_email
// clause in the UPDATE WHERE.
func DecideObservation(ctx context.Context, userEmail string, id int64, approve bool) error {
	status := "rejected"
	if approve {
		status = "approved"
	}
	if err := db.New(store.GetDB()).UpdateCorrectionObservationStatus(ctx, db.UpdateCorrectionObservationStatusParams{
		Status: status, ID: id, UserEmail: userEmail,
	}); err != nil {
		return fmt.Errorf("decide observation %d: %w", id, err)
	}
	return nil
}

// upsertCorrectionObservation records or accumulates evidence for a correction
// pattern. Returns justPromoted=true only on the call that crosses the promotion
// threshold (pending -> promoted), so callers can gate one-time side effects (e.g.
// a negative learned example) on that transition rather than on every evidence hit.
func upsertCorrectionObservation(ctx context.Context, userEmail, kind, from, to, scope string, msgID store.MessageID, threshold int64) (justPromoted bool, err error) {
	q := db.New(store.GetDB())
	obs, err := q.GetCorrectionObservation(ctx, db.GetCorrectionObservationParams{
		UserEmail: userEmail, Kind: kind, FromValue: from, ToValue: to, Scope: scope,
	})
	if errors.Is(err, sql.ErrNoRows) {
		seen, marshalErr := json.Marshal([]int64{int64(msgID)})
		if marshalErr != nil {
			return false, fmt.Errorf("marshal seen_message_ids: %w", marshalErr)
		}
		if insErr := q.InsertCorrectionObservation(ctx, db.InsertCorrectionObservationParams{
			UserEmail: userEmail, Kind: kind, FromValue: from, ToValue: to, Scope: scope,
			EvidenceCount: 1, SeenMessageIds: string(seen), Status: "pending",
		}); insErr != nil {
			return false, fmt.Errorf("insert correction observation: %w", insErr)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get correction observation: %w", err)
	}
	// Why: a rejected pattern must never resurrect from new evidence.
	if obs.Status == "rejected" {
		return false, nil
	}
	var seenIDs []int64
	_ = json.Unmarshal([]byte(obs.SeenMessageIds), &seenIDs)
	if containsInt64(seenIDs, int64(msgID)) {
		return false, nil
	}
	seenIDs = append(seenIDs, int64(msgID))
	newCount := obs.EvidenceCount + 1
	seenJSON, err := json.Marshal(seenIDs)
	if err != nil {
		return false, fmt.Errorf("marshal seen_message_ids: %w", err)
	}
	if err := q.UpdateCorrectionObservationEvidence(ctx, db.UpdateCorrectionObservationEvidenceParams{
		EvidenceCount: newCount, SeenMessageIds: string(seenJSON), ID: obs.ID,
	}); err != nil {
		return false, fmt.Errorf("update correction observation evidence: %w", err)
	}
	if newCount < threshold || obs.Status != "pending" {
		return false, nil
	}
	if err := q.UpdateCorrectionObservationStatus(ctx, db.UpdateCorrectionObservationStatusParams{
		Status: "promoted", ID: obs.ID, UserEmail: userEmail,
	}); err != nil {
		return false, fmt.Errorf("promote correction observation: %w", err)
	}
	// Why: learning must be inspectable -- log the promotion, not just the evidence.
	logger.Infof("[LEARNING] promoted observation kind=%s from=%q to=%q scope=%q evidence=%d", kind, from, to, scope, newCount)
	return true, nil
}

func containsInt64(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func insertLearnedExample(ctx context.Context, userEmail, source, input, expected, origin string, messageID store.MessageID) {
	err := db.New(store.GetDB()).InsertLearnedExample(ctx, db.InsertLearnedExampleParams{
		UserEmail: userEmail, Source: source, Lang: "", Input: input, Expected: expected, Origin: origin,
		MessageID: sql.NullInt64{Int64: int64(messageID), Valid: messageID != 0},
	})
	if err != nil {
		logger.Warnf("[LEARNING] insert learned example origin=%s msg=%d: %v", origin, messageID, err)
	}
}
