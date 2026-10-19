package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
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
)

const (
	FilterDropTextHeadRunes = 400
	filterDropListLimit     = 97
	filterDropMaxDays       = 31
	filterDropDefaultDays   = 7
)

type ReviewAnalyzer interface {
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string) ([]store.TodoItem, error)
}

type taskRouter func(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error)

type FilterReviewService struct {
	analyzer ReviewAnalyzer
	route    taskRouter
	// Why: store has no conditional outcome update, so rescues serialize to stay idempotent under double clicks.
	mu sync.Mutex
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
	return &FilterReviewService{analyzer: analyzer, route: HandleTaskState}
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

func (s *FilterReviewService) Confirm(ctx context.Context, email string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := loadDrop(ctx, email, id)
	if err != nil {
		return err
	}
	// Why: a rescued drop already produced tasks; never downgrade it to noise.
	if row.Outcome == store.FilterOutcomeRescued {
		return nil
	}
	return store.SetFilterOutcome(ctx, email, id, store.FilterOutcomeConfirmedNoise)
}

func (s *FilterReviewService) Rescue(ctx context.Context, email string, id int64) (RescueResult, error) {
	if s.analyzer == nil {
		return RescueResult{}, ErrRescueUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := loadDrop(ctx, email, id)
	if err != nil {
		return RescueResult{}, err
	}
	if row.Outcome == store.FilterOutcomeRescued {
		return RescueResult{AlreadyRescued: true}, nil
	}
	items, err := s.analyzer.Analyze(ctx, email, rescueEnvelope(row), "Korean", row.Source, rescueRoom(row))
	if err != nil {
		return RescueResult{}, fmt.Errorf("rescue analyze: %w", err)
	}
	created := s.routeRescued(ctx, email, row, items)
	// Why: marked after extraction so a failed analyze stays retryable; the false-drop signal holds even when no task results.
	if err := store.SetFilterOutcome(ctx, email, id, store.FilterOutcomeRescued); err != nil {
		return RescueResult{Created: created}, err
	}
	return RescueResult{Created: created}, nil
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

func (s *FilterReviewService) routeRescued(ctx context.Context, email string, row db.FilterScore, items []store.TodoItem) int {
	created := 0
	for _, item := range items {
		msg := store.ConsolidatedMessage{
			UserEmail: email, Source: row.Source, Room: rescueRoom(row), SourceTS: row.SourceTs,
			OriginalText: row.Text, CreatedAt: row.CreatedAt.Time, AssignedAt: row.CreatedAt.Time,
		}
		var id store.MessageID
		err := store.WithDBRetry("FilterReview.HandleTaskState", func() error {
			var e error
			id, e = s.route(ctx, store.GetDB(), email, item, msg)
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
