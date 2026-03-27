package handlers

import (
	"net/http"
	"strconv"

	"message-consolidator/auth"
	"message-consolidator/services"
	"message-consolidator/store"

	"github.com/gorilla/mux"
)

func HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch messages", err)
		return
	}
	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	services.PrepareMessagesForClient(email, msgs, lang)

	respondJSON(w, msgs)
}

func HandleMarkDone(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID   int  `json:"id"`
		Done bool `json:"done"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := services.HandleTaskCompletion(email, req.ID, req.Done); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to complete task", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func HandleGetArchived(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusInternalServerError, "Failed to fetch archived messages", err)
		return
	}

	// DB 쿼리 결과로 새로 할당된 슬라이스이므로 캐시 오염 우려가 없어 복사 불필요
	msgs := msgsRaw

	services.PrepareMessagesForClient(email, msgs, lang)

	respondJSON(w, map[string]interface{}{
		"messages": msgs,
		"total":    total,
	})
}

func HandleGetArchivedCount(w http.ResponseWriter, r *http.Request) {
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
		respondError(w, http.StatusInternalServerError, "Failed to fetch archive count", err)
		return
	}

	respondJSON(w, map[string]int{"count": total})
}

func HandleDelete(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID  int   `json:"id"`
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ids := req.IDs
	if len(ids) == 0 && req.ID != 0 {
		ids = []int{req.ID}
	}

	store.DeleteMessages(email, ids)
	w.WriteHeader(http.StatusOK)
}

func HandleGetOriginal(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	msg, err := store.GetMessageByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch original text", err)
		return
	}

	if msg.UserEmail != email {
		respondError(w, http.StatusUnauthorized, "Unauthorized access", nil)
		return
	}

	respondJSON(w, map[string]string{"original_text": msg.OriginalText})
}

func HandleHardDelete(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store.HardDeleteMessages(email, req.IDs)
	w.WriteHeader(http.StatusOK)
}

func HandleRestore(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	store.RestoreMessages(email, req.IDs)
	w.WriteHeader(http.StatusOK)
}

func HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ID   int    `json:"id"`
		Task string `json:"task"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.UpdateTaskText(email, req.ID, req.Task); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update task", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
