package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"message-consolidator/auth"
	"message-consolidator/db"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// Why: bound the returned example set; matches project convention of prime-numbered caps.
const learnedExamplesLimit = 97

type patchMessageDetailsRequest struct {
	ID       store.MessageID `json:"id"`
	Task     *string         `json:"task,omitempty"`
	Assignee *string         `json:"assignee,omitempty"`
	Deadline *string         `json:"deadline,omitempty"`
	Category *string         `json:"category,omitempty"`
}

func editedFieldNames(req patchMessageDetailsRequest) []string {
	var edited []string
	if req.Task != nil {
		edited = append(edited, "task")
	}
	if req.Assignee != nil {
		edited = append(edited, "assignee")
	}
	if req.Deadline != nil {
		edited = append(edited, "deadline")
	}
	if req.Category != nil {
		edited = append(edited, "category")
	}
	return edited
}

// parseDeadlineDateColumn converts an ISO YYYY-MM-DD string to sql.NullTime for the
// deadline_date column. Empty/unparseable input yields Valid=false.
func parseDeadlineDateColumn(iso string) sql.NullTime {
	if iso == "" {
		return sql.NullTime{}
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// buildCorrectionFields translates the pointer-field request into a store update,
// computing deadline_date/deadline_inferred and merging field_sources metadata.
// Why: known limitation -- COALESCE cannot express "set NULL", so clearing a
// deadline to "" leaves a stale deadline_date; see store/message_update.go
// unmarkMessageDone for the established raw-SQL pattern used elsewhere for this
// exact limitation.
func buildCorrectionFields(req patchMessageDetailsRequest, before store.ConsolidatedMessage) (store.UpdateMessageCorrectionFields, error) {
	var f store.UpdateMessageCorrectionFields
	if req.Task != nil {
		f.Task = sql.NullString{String: *req.Task, Valid: true}
	}
	if req.Assignee != nil {
		f.Assignee = sql.NullString{String: *req.Assignee, Valid: true}
	}
	if req.Category != nil {
		f.Category = sql.NullString{String: strings.ToUpper(*req.Category), Valid: true}
	}
	if req.Deadline != nil {
		f.Deadline = sql.NullString{String: *req.Deadline, Valid: true}
		deadlineDate, inferred := services.ParseDeadline(*req.Deadline, time.Now())
		f.DeadlineDate = parseDeadlineDateColumn(deadlineDate)
		f.DeadlineInferred = sql.NullInt64{Int64: boolToInt64(inferred), Valid: true}
	}
	meta, err := services.MarkFieldSources(before.Metadata, editedFieldNames(req))
	if err != nil {
		return f, fmt.Errorf("mark field sources: %w", err)
	}
	f.Metadata = sql.NullString{String: string(meta), Valid: true}
	return f, nil
}

// HandlePatchMessageDetails applies a user-facing correction to task/assignee/
// deadline/category. Why: distinct from HandleUpdateTask (task-title only) -- this
// is the write path that also marks field_sources so a later AI rescan cannot
// overwrite the human decision, and feeds services.RecordTaskEdit for correction
// learning.
func (a *API) HandlePatchMessageDetails(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req patchMessageDetailsRequest
	if !bindJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Invalid Task ID")
		return
	}
	if req.Category != nil && !types.IsValidTaskCategory(*req.Category) {
		respondError(w, http.StatusBadRequest, "Invalid category")
		return
	}
	edited := editedFieldNames(req)
	if len(edited) == 0 {
		respondError(w, http.StatusBadRequest, "No fields to update")
		return
	}

	before, err := store.GetMessageByID(r.Context(), store.GetDB(), email, req.ID)
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] fetch message for "+email, "Failed to fetch task")
		return
	}
	// Why: strict isolation check to prevent cross-user ID enumeration (see HandleGetOriginal).
	if before.UserEmail != email {
		respondError(w, http.StatusUnauthorized, "Unauthorized access")
		return
	}

	fields, err := buildCorrectionFields(req, before)
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] build correction fields for "+email, "Failed to update task")
		return
	}
	if err := store.UpdateMessageCorrection(r.Context(), store.GetDB(), email, req.ID, fields); err != nil {
		handleAPIError(w, r, err, "[LEARNING] update message correction for "+email, "Failed to update task")
		return
	}

	services.RecordTaskEdit(r.Context(), email, before, services.EditFields{
		Task: req.Task, Assignee: req.Assignee, Deadline: req.Deadline, Category: req.Category,
	})
	w.WriteHeader(http.StatusOK)
}

type createMessageRequest struct {
	Task         string  `json:"task"`
	Assignee     *string `json:"assignee,omitempty"`
	Deadline     *string `json:"deadline,omitempty"`
	Category     *string `json:"category,omitempty"`
	Room         string  `json:"room,omitempty"`
	OriginalText string  `json:"original_text,omitempty"`
}

func manualCreateFieldNames(req createMessageRequest) []string {
	edited := []string{"task"}
	if req.Assignee != nil {
		edited = append(edited, "assignee")
	}
	if req.Deadline != nil {
		edited = append(edited, "deadline")
	}
	if req.Category != nil {
		edited = append(edited, "category")
	}
	return edited
}

// buildManualMessage assembles the ConsolidatedMessage for a manually-created task.
// Why: source="manual" and a synthetic source_ts (no external message backs this row)
// plus field_sources marking every field the user actually supplied (not the
// preferred-name/category defaults) so a later rescan cannot overwrite them.
func buildManualMessage(email string, req createMessageRequest, assignee, category string) (store.ConsolidatedMessage, error) {
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       store.SourceManual,
		Room:         req.Room,
		Task:         req.Task,
		Assignee:     assignee,
		Category:     category,
		SourceTS:     fmt.Sprintf("manual-%d", time.Now().UnixNano()),
		OriginalText: req.OriginalText,
	}
	if req.Deadline != nil {
		msg.Deadline = *req.Deadline
		date, inferred := services.ParseDeadline(*req.Deadline, time.Now())
		msg.DeadlineDate = date
		msg.DeadlineInferred = inferred
	}
	meta, err := services.MarkFieldSources(nil, manualCreateFieldNames(req))
	if err != nil {
		return msg, fmt.Errorf("mark field sources: %w", err)
	}
	msg.Metadata = meta
	return msg, nil
}

// HandleCreateMessage lets the user add a task the AI missed. Why: the highest-value
// correction-learning signal (false negative) -- see services.RecordManualAdd.
func (a *API) HandleCreateMessage(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req createMessageRequest
	if !bindJSON(w, r, &req) {
		return
	}
	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		respondError(w, http.StatusBadRequest, "task is required")
		return
	}
	category := string(types.CategoryTask)
	if req.Category != nil {
		if !types.IsValidTaskCategory(*req.Category) {
			respondError(w, http.StatusBadRequest, "Invalid category")
			return
		}
		category = strings.ToUpper(*req.Category)
	}

	user, err := store.GetOrCreateUser(r.Context(), email, "", "")
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] load user for "+email, "Failed to create task")
		return
	}
	assignee := user.PreferredName()
	if req.Assignee != nil {
		assignee = *req.Assignee
	}

	msg, err := buildManualMessage(email, req, assignee, category)
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] build manual message for "+email, "Failed to create task")
		return
	}

	_, id, err := store.SaveMessage(r.Context(), store.GetDB(), msg)
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] save manual message for "+email, "Failed to create task")
		return
	}
	msg.ID = id

	services.RecordManualAdd(r.Context(), email, msg, req.OriginalText)

	final, err := store.GetMessageByID(r.Context(), store.GetDB(), email, id)
	if err != nil {
		// Why: the write already succeeded -- fall back to the constructed row
		// rather than failing a successful create on a read-back error.
		respondJSON(w, http.StatusOK, msg)
		return
	}
	respondJSON(w, http.StatusOK, final)
}

// HandleListObservations lists learned correction observations by status (default
// "promoted") for the confirm/reject UI.
func (a *API) HandleListObservations(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "promoted"
	}
	rows, err := db.New(store.GetDB()).ListCorrectionObservationsByStatus(r.Context(), db.ListCorrectionObservationsByStatusParams{
		UserEmail: email, Status: status,
	})
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] list observations for "+email, "Failed to list observations")
		return
	}
	respondJSON(w, http.StatusOK, rows)
}

type decideObservationRequest struct {
	ID      int64 `json:"id"`
	Approve bool  `json:"approve"`
}

// HandleDecideObservation records a human approve/reject decision on a pending
// correction observation.
func (a *API) HandleDecideObservation(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req decideObservationRequest
	if !bindJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Invalid observation ID")
		return
	}
	if err := services.DecideObservation(r.Context(), email, req.ID, req.Approve); err != nil {
		handleAPIError(w, r, err, "[LEARNING] decide observation for "+email, "Failed to update observation")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleListLearnedExamples lists the most recent learned few-shot examples.
func (a *API) HandleListLearnedExamples(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	rows, err := db.New(store.GetDB()).ListLearnedExamples(r.Context(), db.ListLearnedExamplesParams{
		UserEmail: email, Limit: learnedExamplesLimit,
	})
	if err != nil {
		handleAPIError(w, r, err, "[LEARNING] list examples for "+email, "Failed to list examples")
		return
	}
	respondJSON(w, http.StatusOK, rows)
}

type deleteLearnedExampleRequest struct {
	ID int64 `json:"id"`
}

// HandleDeleteLearnedExample deletes a learned few-shot example. Why: reversibility --
// a mined example that turns out wrong (e.g. a false-positive completion signal) must
// be removable without direct DB access.
func (a *API) HandleDeleteLearnedExample(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req deleteLearnedExampleRequest
	if !bindJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Invalid example ID")
		return
	}
	if err := db.New(store.GetDB()).DeleteLearnedExample(r.Context(), db.DeleteLearnedExampleParams{
		ID: req.ID, UserEmail: email,
	}); err != nil {
		handleAPIError(w, r, err, "[LEARNING] delete example for "+email, "Failed to delete example")
		return
	}
	w.WriteHeader(http.StatusOK)
}
