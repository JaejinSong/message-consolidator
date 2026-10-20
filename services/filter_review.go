package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"message-consolidator/db"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

var (
	ErrFilterDropNotFound = errors.New("filter drop not found")
	ErrFilterNotADrop     = errors.New("filter score is not a drop")
	ErrRescueUnavailable  = errors.New("rescue extraction unavailable")
	ErrRescueContended    = errors.New("rescue contended by concurrent updates")
)

const (
	FilterDropTextHeadRunes = 400
	filterDropListLimit     = 97
	filterDropMaxDays       = 31
	filterDropDefaultDays   = 7
	rescueClaimAttempts     = 3
	rescueRestoreTimeout    = 7 * time.Second
)

type ReviewAnalyzer interface {
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string) ([]store.TodoItem, error)
}

type taskRouter func(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error)

type FilterReviewService struct {
	analyzer ReviewAnalyzer
	route    taskRouter
	// Why: injectable so tests avoid the user/alias tables; returns the effective alias set.
	loadUser func(ctx context.Context, email string) (store.User, []string, error)
}

type FilterDrop struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	Room        string    `json:"room"`
	PActionable float64   `json:"p_actionable"`
	Category    string    `json:"category"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	Outcome     string    `json:"outcome"`
}

type RescueResult struct {
	AlreadyRescued bool `json:"already_rescued"`
	Created        int  `json:"created"`
}

func NewFilterReviewService(analyzer ReviewAnalyzer) *FilterReviewService {
	// Why: a nil *GeminiClient boxed into the interface is non-nil, so callers pass an untyped nil.
	return &FilterReviewService{analyzer: analyzer, route: HandleTaskState, loadUser: loadRescueUser}
}

func (s *FilterReviewService) ListDrops(ctx context.Context, email string, days int, now time.Time) ([]FilterDrop, error) {
	if days <= 0 {
		days = filterDropDefaultDays
	}
	if days > filterDropMaxDays {
		days = filterDropMaxDays
	}
	rows, err := store.ListFilterDrops(ctx, email, now.AddDate(0, 0, -days), filterDropListLimit)
	if err != nil {
		return nil, fmt.Errorf("list drops: %w", err)
	}
	drops := make([]FilterDrop, 0, len(rows))
	for _, r := range rows {
		drops = append(drops, FilterDrop{
			ID: r.ID, Source: r.Source, Room: r.Room, PActionable: r.PActionable, Category: r.Category,
			Text: headRunes(r.Text, FilterDropTextHeadRunes), CreatedAt: r.CreatedAt.Time, Outcome: r.Outcome,
		})
	}
	return drops, nil
}

func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func loadDrop(ctx context.Context, email string, id int64) (db.FilterScore, error) {
	row, err := store.GetFilterScore(ctx, email, id)
	if errors.Is(err, sql.ErrNoRows) {
		return db.FilterScore{}, ErrFilterDropNotFound
	}
	if err != nil {
		return db.FilterScore{}, fmt.Errorf("load drop: %w", err)
	}
	if row.Verdict != store.FilterVerdictNoise {
		return db.FilterScore{}, ErrFilterNotADrop
	}
	return row, nil
}

func loadRescueUser(ctx context.Context, email string) (store.User, []string, error) {
	user, err := store.GetOrCreateUser(ctx, email, "", "")
	if err != nil {
		return store.User{}, nil, fmt.Errorf("load user: %w", err)
	}
	aliases, err := store.GetUserAliases(ctx, user.ID)
	if err != nil {
		return store.User{}, nil, fmt.Errorf("load aliases: %w", err)
	}
	return *user, GetEffectiveAliases(*user, aliases), nil
}

func (s *FilterReviewService) Confirm(ctx context.Context, email string, id int64) error {
	if _, err := loadDrop(ctx, email, id); err != nil {
		return err
	}
	// Why: a rescued drop already produced tasks; the conditional update never downgrades it.
	return store.ConfirmFilterNoise(ctx, email, id)
}

func (s *FilterReviewService) Rescue(ctx context.Context, email string, id int64) (RescueResult, error) {
	if s.analyzer == nil {
		return RescueResult{}, ErrRescueUnavailable
	}
	row, claimed, err := claimRescue(ctx, email, id)
	if err != nil {
		return RescueResult{}, err
	}
	if !claimed {
		return RescueResult{AlreadyRescued: true}, nil
	}
	created, err := s.extractRescued(ctx, email, row)
	if err != nil {
		// Why: the request ctx may be the very reason extraction failed, and the revert must still land to keep the drop retryable.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rescueRestoreTimeout)
		defer cancel()
		if rerr := store.RestoreFilterOutcome(rctx, email, id, row.Outcome); rerr != nil {
			logger.Warnf("[FILTER-REVIEW] restore outcome failed for %s id=%d: %v", email, id, rerr)
		}
		return RescueResult{}, err
	}
	return RescueResult{Created: created}, nil
}

// Why: the claim compares against the outcome just read, so a concurrent Confirm is retried instead of overwritten.
func claimRescue(ctx context.Context, email string, id int64) (db.FilterScore, bool, error) {
	for range rescueClaimAttempts {
		row, err := loadDrop(ctx, email, id)
		if err != nil {
			return db.FilterScore{}, false, err
		}
		if row.Outcome == store.FilterOutcomeRescued {
			return row, false, nil
		}
		claimed, err := store.ClaimFilterRescue(ctx, email, id, row.Outcome)
		if err != nil {
			return db.FilterScore{}, false, err
		}
		if claimed {
			return row, true, nil
		}
	}
	return db.FilterScore{}, false, ErrRescueContended
}

func (s *FilterReviewService) extractRescued(ctx context.Context, email string, row db.FilterScore) (int, error) {
	user, aliases, err := s.loadUser(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("rescue user: %w", err)
	}
	items, err := s.analyzer.Analyze(ctx, email, rescueEnvelope(row), "Korean", row.Source, rescueRoom(row))
	if err != nil {
		return 0, fmt.Errorf("rescue analyze: %w", err)
	}
	return s.routeRescued(ctx, email, user, aliases, row, items), nil
}

func rescueRoom(row db.FilterScore) string {
	// Why: Gmail rows are scored without a room, but Gmail tasks live in room "Gmail".
	if row.Source == store.SourceGmail {
		return "Gmail"
	}
	if row.Room == "" {
		return "General"
	}
	return row.Room
}

func rescueEnvelope(row db.FilterScore) types.EnrichedMessage {
	return types.EnrichedMessage{
		RawContent: row.Text, SourceChannel: row.Source, Timestamp: row.CreatedAt.Time,
	}
}

func (s *FilterReviewService) routeRescued(ctx context.Context, email string, user store.User, aliases []string, row db.FilterScore, items []store.TodoItem) int {
	created := 0
	for _, item := range items {
		guarded, msg, guard, ok := GuardAndBuild(ctx, TaskBuildParams{
			UserEmail: email, User: user, Aliases: aliases, Item: item,
			Source: row.Source, Room: rescueRoom(row), SourceTS: row.SourceTs,
			Timestamp: row.CreatedAt.Time, OriginalText: row.Text, SourceChannels: []string{row.Source},
		})
		if !ok {
			logger.Infof("[FILTER-REVIEW] extraction guard dropped rescued item for %s (%s)", email, guard.DropReason)
			continue
		}
		var id store.MessageID
		err := store.WithDBRetry("FilterReview.HandleTaskState", func() error {
			var e error
			id, e = s.route(ctx, store.GetDB(), email, guarded, msg)
			return e
		})
		if err != nil {
			logger.Warnf("[FILTER-REVIEW] rescue route dropped item for %s: %v", email, err)
			continue
		}
		if id > 0 {
			created++
		}
	}
	return created
}
