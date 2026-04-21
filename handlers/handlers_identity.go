package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/store"
	"net/http"

	"github.com/gorilla/mux"
)

// HandleGenerateProposals triggers an AI scan of the tenant's contacts and stores new merge proposals.
// AI-proposed groups are skipped when every pair in the group has already been handled (pending/rejected).
func (a *API) HandleGenerateProposals(w http.ResponseWriter, r *http.Request) {
	if a.IdentityResolver == nil {
		respondError(w, http.StatusServiceUnavailable, "AI identity resolver not configured")
		return
	}

	email := auth.GetUserEmail(r)
	contacts, err := store.GetStandaloneContacts(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[Identity]", "Failed to load contacts")
		return
	}

	// Load all previously handled pairs so we can skip fully-redundant AI proposals.
	handledPairs, err := store.LoadHandledPairs(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[Identity]", "Failed to load handled pairs")
		return
	}

	groups, err := a.IdentityResolver.ProposeGroups(r.Context(), contacts)
	if err != nil {
		handleAPIError(w, r, err, "[Identity]", "AI proposal generation failed")
		return
	}

	inserted := 0
	for _, g := range groups {
		if len(g.ContactIDs) < 2 {
			continue
		}
		// Skip groups where every pair is already recorded — nothing new to propose.
		if allPairsHandled(g.ContactIDs, handledPairs) {
			continue
		}
		groupID := store.NewGroupID()
		if err := store.InsertProposalGroup(r.Context(), groupID, g.ContactIDs, g.Confidence, g.Reason); err != nil {
			handleAPIError(w, r, err, "[Identity]", "Failed to save proposal")
			return
		}
		inserted++
	}

	respondJSON(w, http.StatusOK, map[string]int{"proposals_created": inserted})
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
