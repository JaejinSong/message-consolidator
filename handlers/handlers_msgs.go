package handlers

import (
	"context"
	"errors"
	"message-consolidator/auth"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	lang := r.URL.Query().Get("lang")

	msgsRaw, err := store.GetMessages(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	applyTranslations(msgs, lang)

	for i := range msgs {
		msgs[i].HasOriginal = msgs[i].OriginalText != ""
		msgs[i].OriginalText = "" // 페이로드 압축을 위해 원문 제거 (Lazy Loading)
	}

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

	if err := services.HandleTaskCompletion(email, req.ID, req.Done); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	if limit <= 0 {
		limit = 50
	}

	msgsRaw, total, err := store.GetArchivedMessagesFiltered(r.Context(), email, limit, offset, q, sort, order)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			http.Error(w, "Client Closed Request", 499)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	msgs := make([]store.ConsolidatedMessage, len(msgsRaw))
	copy(msgs, msgsRaw)

	applyTranslations(msgs, lang)

	for i := range msgs {
		msgs[i].HasOriginal = msgs[i].OriginalText != ""
		msgs[i].OriginalText = ""
	}

	respondJSON(w, map[string]interface{}{
		"messages": msgs,
		"total":    total,
	})
}

func HandleGetArchivedCount(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	q := r.URL.Query().Get("q")

	_, total, err := store.GetArchivedMessagesFiltered(r.Context(), email, 1, 0, q, "", "")
	if err != nil {
		if errors.Is(err, context.Canceled) {
			http.Error(w, "Client Closed Request", 499)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]int{"total": total})
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
		if errors.Is(err, context.Canceled) {
			http.Error(w, "Client Closed Request", 499)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if msg.UserEmail != email {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
