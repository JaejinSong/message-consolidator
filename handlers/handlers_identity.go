package handlers

import (
	"context"
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

type proposalJob struct {
	Status     string `json:"status"` // running | done | error | idle
	Count      int    `json:"proposals_created,omitempty"`
	AutoMerged int    `json:"auto_merged,omitempty"`
	ErrMsg     string `json:"error,omitempty"`
}

var (
	proposalJobsMu sync.Mutex
	proposalJobs   = map[string]*proposalJob{}
)

// HandleGenerateProposals triggers an async AI scan of the tenant's contacts.
// Returns 202 immediately; poll /api/identity/proposals/job-status for completion.
func (a *API) HandleGenerateProposals(w http.ResponseWriter, r *http.Request) {
	if a.IdentityResolver == nil {
		respondError(w, http.StatusServiceUnavailable, "AI identity resolver not configured")
		return
	}

	email := auth.GetUserEmail(r)

	proposalJobsMu.Lock()
	if j, ok := proposalJobs[email]; ok && j.Status == "running" {
		proposalJobsMu.Unlock()
		respondError(w, http.StatusConflict, "Analysis already in progress")
		return
	}
	proposalJobs[email] = &proposalJob{Status: "running"}
	proposalJobsMu.Unlock()

	go func() { //nolint:contextcheck // Async job uses Background ctx by design; lifecycle outlives request.
		result := a.runProposalJob(email)
		proposalJobsMu.Lock()
		proposalJobs[email] = result
		proposalJobsMu.Unlock()
	}()

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
}

func (a *API) runProposalJob(email string) *proposalJob {
	ctx := context.Background()

	autoMerged, err := store.AutoMergeByCanonicalID(ctx, email)
	if err != nil {
		return proposalJobError(err)
	}
	contacts, err := store.GetCandidateContacts(ctx, email)
	if err != nil {
		return proposalJobError(err)
	}
	handledPairs, err := store.LoadHandledPairs(ctx, email)
	if err != nil {
		return proposalJobError(err)
	}

	aiInserted, err := a.insertAIProposalGroups(ctx, contacts, handledPairs)
	if err != nil {
		return proposalJobError(err)
	}
	tokenInserted, err := insertTokenSortedProposals(ctx, email, handledPairs)
	if err != nil {
		return proposalJobError(err)
	}

	return &proposalJob{Status: "done", Count: aiInserted + tokenInserted, AutoMerged: autoMerged}
}

func proposalJobError(err error) *proposalJob {
	return &proposalJob{Status: "error", ErrMsg: err.Error()}
}

func (a *API) insertAIProposalGroups(ctx context.Context, contacts []store.ContactRecord, handledPairs map[[2]int64]bool) (int, error) {
	groups, err := a.IdentityResolver.ProposeGroups(ctx, contacts)
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, g := range groups {
		if len(g.ContactIDs) < 2 || allPairsHandled(g.ContactIDs, handledPairs) {
			continue
		}
		if err := store.InsertProposalGroup(ctx, store.NewGroupID(), g.ContactIDs, g.Confidence, g.Reason); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

// insertTokenSortedProposals catches reversed-order names (e.g. "Phathit Chulothok" ↔ "Chulothok Phathit").
func insertTokenSortedProposals(ctx context.Context, email string, handledPairs map[[2]int64]bool) (int, error) {
	proposals, err := store.GenerateTokenSortedProposals(ctx, email, handledPairs)
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, p := range proposals {
		if allPairsHandled(p.ContactIDs, handledPairs) {
			continue
		}
		if err := store.InsertProposalGroup(ctx, store.NewGroupID(), p.ContactIDs, p.Confidence, p.Reason); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

// HandleProposalJobStatus returns the current async job state for the authenticated user.
func (a *API) HandleProposalJobStatus(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	proposalJobsMu.Lock()
	job, ok := proposalJobs[email]
	proposalJobsMu.Unlock()
	if !ok {
		respondJSON(w, http.StatusOK, &proposalJob{Status: "idle"})
		return
	}
	respondJSON(w, http.StatusOK, job)
}

// HandleListProposals returns all pending merge proposals for the authenticated user's tenant.
func (a *API) HandleListProposals(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	proposals, err := store.ListPendingProposalGroups(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[Identity]", "Failed to list proposals")
		return
	}
	if proposals == nil {
		proposals = []store.ProposalGroup{}
	}
	respondJSON(w, http.StatusOK, proposals)
}

// HandleAcceptProposal accepts a merge proposal and links all contacts under the chosen canonical name.
func (a *API) HandleAcceptProposal(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	groupID := mux.Vars(r)["id"]

	var req struct {
		CanonicalName string `json:"canonical_name"`
	}
	if !bindJSON(w, r, &req) {
		return
	}

	if err := store.AcceptProposalGroup(r.Context(), email, groupID, req.CanonicalName); err != nil {
		handleAPIError(w, r, err, "[Identity]", "Failed to accept proposal")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleRejectProposal marks a merge proposal as rejected without linking any contacts.
func (a *API) HandleRejectProposal(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	groupID := mux.Vars(r)["id"]

	if err := store.RejectProposalGroup(r.Context(), email, groupID); err != nil {
		handleAPIError(w, r, err, "[Identity]", "Failed to reject proposal")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// allPairsHandled reports whether every contact pair in ids is already in the handled set.
func allPairsHandled(ids []int64, handled map[[2]int64]bool) bool {
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := ids[i], ids[j]
			if a > b {
				a, b = b, a
			}
			if !handled[[2]int64{a, b}] {
				return false
			}
		}
	}
	return true
}
