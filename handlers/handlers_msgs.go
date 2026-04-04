package handlers

import (
	"net/http"
	"strconv"

	"message-consolidator/auth"
	"message-consolidator/store"

	"github.com/gorilla/mux"
)

func (a *API) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch messages")
		return
	}

	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)
	if a.Tasks != nil {
		a.Tasks.PrepareMessagesForClient(email, msgs, r.URL.Query().Get("lang"))
	}

	name, _ := store.GetUserName(email)
	aliases, _ := store.GetUserAliasesByEmail(email)
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

	if _, err := a.Tasks.HandleTaskCompletion(email, req.ID, req.Done); err != nil {
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
		a.Tasks.PrepareMessagesForClient(email, msgs, lang)
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

	store.DeleteMessages(email, ids)
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

	msg, err := store.GetMessageByID(r.Context(), id)
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
	store.HardDeleteMessages(email, req.IDs)
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
	store.RestoreMessages(email, req.IDs)
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
	if err := store.UpdateTaskText(email, req.ID, req.Task); err != nil {
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

	if err := store.MergeTasks(email, req.TargetIDs, req.DestinationID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to merge tasks: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// HandleTranslateBatchTasks handles JIT translation requests for a batch of tasks.
// It leverages the TasksService for concurrent AI processing and error handling.
func (a *API) HandleTranslateBatchTasks(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		TaskIDs []int  `json:"task_ids"`
		Lang    string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request format: "+err.Error())
		return
	}

	if a.Tasks == nil {
		respondError(w, http.StatusServiceUnavailable, "Task translation service is currently unavailable")
		return
	}

	// [N+1 API Prevention] We process all visible tasks in a single concurrent batch.
	results, err := a.Tasks.ProcessBatchTranslation(r.Context(), email, req.TaskIDs, req.Lang)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to process task translations")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
	})
}
