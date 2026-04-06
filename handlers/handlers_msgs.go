package handlers

import (
	"context"
	"net/http"
	"strconv"

	"message-consolidator/auth"
	"message-consolidator/services"
	"message-consolidator/store"

	"github.com/gorilla/mux"
)

func (a *API) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	msgsRaw, err := store.GetMessages(r.Context(), email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch messages")
		return
	}

	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)
	if a.Tasks != nil {
		a.Tasks.PrepareMessagesForClient(r.Context(), email, msgs, r.URL.Query().Get("lang"))
	}

	name, _ := store.GetUserName(r.Context(), email)
	aliases, _ := store.GetUserAliasesByEmail(r.Context(), email)
	res := struct {
		Inbox   []store.ConsolidatedMessage `json:"inbox"`
		Pending []store.ConsolidatedMessage `json:"pending"`
		Waiting []store.ConsolidatedMessage `json:"waiting"`
	}{
		Inbox:   make([]store.ConsolidatedMessage, 0),
		Pending: make([]store.ConsolidatedMessage, 0),
		Waiting: make([]store.ConsolidatedMessage, 0),
	}

	for _, m := range msgs {
		if m.Category == "waiting" {
			res.Waiting = append(res.Waiting, m)
		} else if store.IsAssignedToUser(m.Assignee, name, aliases) {
			res.Inbox = append(res.Inbox, m)
		} else {
			res.Pending = append(res.Pending, m)
		}
	}
	respondJSON(w, http.StatusOK, res)
}

func (a *API) HandleMarkDone(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task service not available")
		return
	}

	if _, err := a.Tasks.HandleTaskCompletion(r.Context(), email, req.ID, req.Done); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to complete task")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetArchived(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	lang := r.URL.Query().Get("lang")
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "all"
	}

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit <= 0 {
		limit = 50
	}

	filter := store.ArchiveFilter{
		Email:  email,
		Limit:  limit,
		Offset: offset,
		Query:  q,
		Sort:   sort,
		Order:  order,
		Status: status,
	}
	msgsRaw, total, err := store.GetArchivedMessagesFiltered(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch archived messages")
		return
	}

	//Why: Uses the direct database result instead of a copy because newly allocated query slices are already safe from cache contamination.
	msgs := msgsRaw

	if a.Tasks != nil {
		a.Tasks.PrepareMessagesForClient(r.Context(), email, msgs, lang)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"messages": msgs,
		"total":    total,
	})
}

func (a *API) HandleGetArchivedCount(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "all"
	}

	filter := store.ArchiveFilter{
		Email:  email,
		Query:  q,
		Status: status,
	}
	total, err := store.GetArchivedMessagesCount(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch archive count")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"count": total})
}

func (a *API) HandleDelete(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID  int   `json:"id"`
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	//Why: Provides backward compatibility for older clients by falling back to a single ID if the batch IDs list is empty.
	ids := req.IDs
	if len(ids) == 0 && req.ID != 0 {
		ids = []int{req.ID}
	}

	_ = store.DeleteMessages(r.Context(), email, ids)
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetOriginal(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	msg, err := store.GetMessageByID(r.Context(), email, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch original text")
		return
	}

	if msg.UserEmail != email {
		respondError(w, http.StatusUnauthorized, "Unauthorized access")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"original_text": msg.OriginalText})
}

func (a *API) HandleHardDelete(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = store.HardDeleteMessages(r.Context(), email, req.IDs)
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleRestore(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = store.RestoreMessages(r.Context(), email, req.IDs)
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID   int    `json:"id"`
		Task string `json:"task"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.UpdateTaskText(r.Context(), email, req.ID, req.Task); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update task")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleMergeTasks consolidates multiple tasks into a single destination task.
// Why: Provides a manual mechanism to correct AI deduplication failures by merging related context.
func (a *API) HandleMergeTasks(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		TargetIDs     []int `json:"target_ids"`
		DestinationID int   `json:"destination_id"`
	}

	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if len(req.TargetIDs) == 0 || req.DestinationID == 0 {
		respondError(w, http.StatusBadRequest, "Missing target_ids or destination_id")
		return
	}

	// Why: [Logic Delegation] Delegates task logic to services to ensure AI summary and transaction integrity.
	targetIDs64 := a.toInt64Slice(req.TargetIDs)
	if err := a.Tasks.MergeTasks(r.Context(), email, targetIDs64, int64(req.DestinationID)); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to merge tasks: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// HandleTranslateBatchTasks handles JIT translation requests for a batch of tasks.
// Why: Implements a cost-efficient cache-first pattern to minimize redundant AI calls.
func (a *API) HandleTranslateBatchTasks(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		TaskIDs []int  `json:"task_ids"`
		Lang    string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request format")
		return
	}

	// 1. [Pre-filter] Get existing translations from DB/Cache
	cached, _ := store.GetTaskTranslationsBatch(r.Context(), req.TaskIDs, req.Lang)
	missingIDs := a.getMissingIDs(req.TaskIDs, cached)

	// [Guard Clause] If all tasks are already translated, return immediately.
	if len(missingIDs) == 0 {
		a.respondWithResults(w, req.TaskIDs, cached, nil, nil)
		return
	}

	// 2. [Batch AI] Translate only missing tasks
	missingReqs := a.prepareMissingRequests(r.Context(), email, missingIDs)
	newTrans, err := a.Tasks.GetTranslationService().TranslateBatch(r.Context(), email, missingReqs, req.Lang)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Translation service failed")
		return
	}

	// 3. [Partial Success] Save only entries without errors.
	successMap := make(map[int]string)
	errorMap := make(map[int]string)
	for _, rt := range newTrans {
		if rt.Error == "" {
			successMap[rt.MessageID] = rt.Text
		} else {
			errorMap[rt.MessageID] = rt.Error
		}
	}

	if len(successMap) > 0 {
		_ = store.SaveTaskTranslationsBulk(r.Context(), req.Lang, successMap)
	}

	a.respondWithResults(w, req.TaskIDs, cached, successMap, errorMap)
}

func (a *API) getMissingIDs(all []int, cached map[int]string) []int {
	var missing []int
	for _, id := range all {
		if _, ok := cached[id]; !ok { missing = append(missing, id) }
	}
	return missing
}

func (a *API) prepareMissingRequests(ctx context.Context, email string, ids []int) []store.TranslateRequest {
	var reqs []store.TranslateRequest
	for _, id := range ids {
		msg, err := store.GetMessageByID(ctx, email, id)
		if err == nil {
			reqs = append(reqs, store.TranslateRequest{ID: id, Text: msg.Task})
		}
	}
	return reqs
}

func (a *API) respondWithResults(w http.ResponseWriter, ids []int, cached, newlyTrans, errors map[int]string) {
	results := make([]services.BatchTranslateResult, len(ids))
	for i, id := range ids {
		text, ok := cached[id]
		if !ok { text = newlyTrans[id] }
		
		results[i] = services.BatchTranslateResult{
			ID:             id,
			Success:        text != "",
			TranslatedText: text,
			Error:          errors[id],
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

func (a *API) toInt64Slice(ids []int) []int64 {
	res := make([]int64, len(ids))
	for i, id := range ids { res[i] = int64(id) }
	return res
}
