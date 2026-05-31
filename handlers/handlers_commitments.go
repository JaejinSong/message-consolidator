package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/db"
	"message-consolidator/store"
	"net/http"
	"time"
)

// CommitmentItem is the client-facing shape for a single commitment row.
type CommitmentItem struct {
	ID                 int64  `json:"id"`
	Task               string `json:"task"`
	Requester          string `json:"requester"`
	Assignee           string `json:"assignee"`
	Category           string `json:"category"`
	Deadline           string `json:"deadline,omitempty"`
	DeadlineDate       string `json:"deadline_date,omitempty"`
	DeadlineInferred   bool   `json:"deadline_inferred,omitempty"`
	Room               string `json:"room"`
	Source             string `json:"source"`
	Link               string `json:"link,omitempty"`
	DaysOpen           int    `json:"days_open"`
}

// StalledItem is the client-facing shape for a stalled TASK row.
type StalledItem struct {
	ID          int64  `json:"id"`
	Task        string `json:"task"`
	Requester   string `json:"requester"`
	Assignee    string `json:"assignee"`
	Room        string `json:"room"`
	Source      string `json:"source"`
	Link        string `json:"link,omitempty"`
	DaysStalled int    `json:"days_stalled"`
}

// StalledBucketsResponse groups stalled tasks into mine / observed buckets.
type StalledBucketsResponse struct {
	Mine     []StalledItem `json:"mine"`
	Observed []StalledItem `json:"observed"`
}

// CommitmentsResponse groups commitments into overdue / undated / active buckets + stalled.
type CommitmentsResponse struct {
	Overdue []CommitmentItem       `json:"overdue"`
	Undated []CommitmentItem       `json:"undated"`
	Active  []CommitmentItem       `json:"active"`
	Stalled StalledBucketsResponse `json:"stalled"`
}

// HandleGetCommitments returns open PROMISE/WAITING rows for the authed user.
// Query param view=mine (default) returns PROMISE items assigned to the user;
// view=waiting returns WAITING items where the user is the requester.
func (a *API) HandleGetCommitments(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	if email == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	view := r.URL.Query().Get("view")
	if view == "" {
		view = "mine"
	}

	rows, err := db.New(store.GetDB()).SelectCommitments(r.Context(), db.SelectCommitmentsParams{
		UserEmail:          email,
		AssigneeCanonical:  email,
		RequesterCanonical: email,
	})
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	resp := CommitmentsResponse{
		Overdue: []CommitmentItem{},
		Undated: []CommitmentItem{},
		Active:  []CommitmentItem{},
		Stalled: StalledBucketsResponse{Mine: []StalledItem{}, Observed: []StalledItem{}},
	}

	for _, row := range rows {
		if !matchesView(view, row) {
			continue
		}
		item := toCommitmentItem(row, today)
		switch commitmentBucket(row, today) {
		case "overdue":
			resp.Overdue = append(resp.Overdue, item)
		case "undated":
			resp.Undated = append(resp.Undated, item)
		default:
			resp.Active = append(resp.Active, item)
		}
	}

	stalledBuckets, err := store.SelectStalled(r.Context(), email, 3)
	if err == nil {
		for _, s := range stalledBuckets.Mine {
			resp.Stalled.Mine = append(resp.Stalled.Mine, toStalledItem(s))
		}
		for _, s := range stalledBuckets.Observed {
			resp.Stalled.Observed = append(resp.Stalled.Observed, toStalledItem(s))
		}
	}

	respondJSON(w, http.StatusOK, resp)
}

func matchesView(view string, row db.SelectCommitmentsRow) bool {
	switch view {
	case "mine":
		return row.Category == "PROMISE"
	case "waiting":
		return row.Category == "WAITING"
	}
	return true
}

func commitmentBucket(row db.SelectCommitmentsRow, today time.Time) string {
	if !row.DeadlineDate.Valid {
		return "undated"
	}
	if row.DeadlineDate.Time.Before(today) {
		return "overdue"
	}
	return "active"
}

func toCommitmentItem(row db.SelectCommitmentsRow, today time.Time) CommitmentItem {
	ddStr := ""
	if row.DeadlineDate.Valid {
		ddStr = row.DeadlineDate.Time.Format("2006-01-02")
	}
	daysOpen := 0
	if row.CreatedAt.Valid {
		daysOpen = int(today.Sub(row.CreatedAt.Time.Truncate(24*time.Hour)).Hours() / 24)
	}
	return CommitmentItem{
		ID:               row.ID,
		Task:             row.Task,
		Requester:        row.Requester,
		Assignee:         row.Assignee,
		Category:         row.Category,
		Deadline:         row.Deadline,
		DeadlineDate:     ddStr,
		DeadlineInferred: row.DeadlineInferred > 0,
		Room:             row.Room,
		Source:           row.Source,
		Link:             row.Link,
		DaysOpen:         daysOpen,
	}
}

func toStalledItem(s store.StalledRequest) StalledItem {
	return StalledItem{
		ID:          int64(s.ID),
		Task:        s.Task,
		Requester:   s.Requester,
		Assignee:    s.Assignee,
		Room:        s.Room,
		Source:      s.Source,
		Link:        s.Link,
		DaysStalled: s.DaysStalled,
	}
}
