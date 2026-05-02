package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"message-consolidator/auth"
)

// minSemanticSearchRunes mirrors minActiveSearchRunes — Why: trigram FTS leg of
// the hybrid ranker still needs ≥3 runes to match anything, so requiring the
// same cutoff keeps the contract consistent with HandleSearchActive.
const minSemanticSearchRunes = 3

// HandleSemanticArchiveSearch runs FTS5 ∪ cosine over the user's archive and
// returns the fused top results in the standard archive envelope.
//
//	GET /api/messages/archive/semantic?q=...&limit=50
func (a *API) HandleSemanticArchiveSearch(w http.ResponseWriter, r *http.Request) {
	if a.Embeddings == nil {
		respondError(w, http.StatusServiceUnavailable, "semantic search not available")
		return
	}
	email := auth.GetUserEmail(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) < minSemanticSearchRunes {
		respondError(w, http.StatusBadRequest, "query must be at least 3 characters")
		return
	}
	limit := defaultArchivePageSize
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		if v > maxArchivePageSize {
			v = maxArchivePageSize
		}
		limit = v
	}

	msgs, err := a.Embeddings.SearchHybrid(r.Context(), email, q, limit)
	if err != nil {
		handleAPIError(w, r, err, "[SEMANTIC] Search error for "+email, "Failed to run semantic search")
		return
	}
	if a.Tasks != nil {
		a.Tasks.PrepareMessagesForClient(r.Context(), email, msgs, r.URL.Query().Get("lang"))
	}
	respondJSON(w, http.StatusOK, archivedMessagesResponse{Messages: msgs, Total: len(msgs)})
}

// embeddingBackfillResponse is the shape returned to the admin UI / curl probes.
type embeddingBackfillResponse struct {
	Processed int `json:"processed"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
	Remaining int `json:"remaining"`
}

// HandleBackfillEmbeddings triggers a bounded batch of archive-message embedding
// generation for the requesting admin (or, when ?email=user is passed, for that
// tenant's archive). Repeated calls drain the missing queue.
//
//	POST /api/admin/embeddings/backfill?batch=100&email=user
func (a *API) HandleBackfillEmbeddings(w http.ResponseWriter, r *http.Request) {
	if a.Embeddings == nil {
		respondError(w, http.StatusServiceUnavailable, "semantic search not available")
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("email"))
	if target == "" {
		target = auth.GetUserEmail(r)
	}
	batch := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("batch")); err == nil && v > 0 {
		// Why: cap to keep one request inside the e2-micro RAM budget and Gemini
		// per-minute call quotas — operators chain calls for larger sweeps.
		if v > 500 {
			v = 500
		}
		batch = v
	}

	processed, skipped, failed, err := a.Embeddings.BackfillBatch(r.Context(), target, batch)
	if err != nil {
		handleAPIError(w, r, err, "[EMBED] Backfill error for "+target, "Failed to run backfill")
		return
	}
	remaining, _ := a.Embeddings.CountMissing(r.Context(), target)
	respondJSON(w, http.StatusOK, embeddingBackfillResponse{
		Processed: processed,
		Skipped:   skipped,
		Failed:    failed,
		Remaining: remaining,
	})
}
