package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"message-consolidator/auth"
	"message-consolidator/services"
)

type filterDropRequest struct {
	ID int64 `json:"id"`
}

func (a *API) filterReviewSvc() *services.FilterReviewService {
	if a.FilterReview != nil {
		return a.FilterReview
	}
	return services.NewFilterReviewService(nil)
}

func respondFilterReviewError(w http.ResponseWriter, r *http.Request, err error, email, op string) {
	switch {
	case errors.Is(err, services.ErrFilterDropNotFound):
		respondError(w, http.StatusNotFound, "Filtered message not found")
	case errors.Is(err, services.ErrFilterNotADrop):
		respondError(w, http.StatusBadRequest, "Message was not filtered")
	case errors.Is(err, services.ErrRescueUnavailable):
		respondError(w, http.StatusServiceUnavailable, "AI extraction is not available")
	default:
		handleAPIError(w, r, err, "[FILTER-REVIEW] "+op+" for "+email, "Failed to "+op)
	}
}

// HandleListFilterDrops lists recent noise-filter drops of the caller for review.
func (a *API) HandleListFilterDrops(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	drops, err := a.filterReviewSvc().ListDrops(r.Context(), email, days, time.Now())
	if err != nil {
		respondFilterReviewError(w, r, err, email, "list filtered messages")
		return
	}
	respondJSON(w, http.StatusOK, drops)
}

// HandleRescueFilterDrop re-runs extraction on a dropped message (false-drop signal).
func (a *API) HandleRescueFilterDrop(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req filterDropRequest
	if !bindJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Invalid filter drop ID")
		return
	}
	res, err := a.filterReviewSvc().Rescue(r.Context(), email, req.ID)
	if err != nil {
		respondFilterReviewError(w, r, err, email, "rescue filtered message")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// HandleConfirmFilterDrop records that a dropped message was correctly filtered.
func (a *API) HandleConfirmFilterDrop(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req filterDropRequest
	if !bindJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		respondError(w, http.StatusBadRequest, "Invalid filter drop ID")
		return
	}
	if err := a.filterReviewSvc().Confirm(r.Context(), email, req.ID); err != nil {
		respondFilterReviewError(w, r, err, email, "confirm filtered message")
		return
	}
	w.WriteHeader(http.StatusOK)
}
