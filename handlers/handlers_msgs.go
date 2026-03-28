package handlers

import (
	"net/http"
	"strconv"

	"message-consolidator/auth"
	"message-consolidator/services"
	"message-consolidator/store"

	"github.com/gorilla/mux"
)

func (a *API) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch messages")
		return
	}
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	services.PrepareMessagesForClient(email, msgs, lang)

	respondJSON(w, http.StatusOK, msgs)
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

	if _, err := services.HandleTaskCompletion(email, req.ID, req.Done); err != nil {
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

	services.PrepareMessagesForClient(email, msgs, lang)

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
