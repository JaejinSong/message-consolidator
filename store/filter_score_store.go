package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"message-consolidator/db"
)

// Filter score verdicts and outcomes. Kept as constants so queries and callers agree.
const (
	FilterVerdictPass  = "pass"
	FilterVerdictNoise = "noise"

	FilterOutcomeTask           = "task"
	FilterOutcomeNone           = "none"
	FilterOutcomeRescued        = "rescued"
	FilterOutcomeConfirmedNoise = "confirmed_noise"
	FilterOutcomeDeletedNoise   = "deleted_noise"
	FilterOutcomeManualMatch    = "manual_match"
)

// FilterScoreTextMaxRunes caps the stored text. Why: rescue re-runs extraction from this copy,
// so it must hold a whole group payload, yet stay bounded.
const FilterScoreTextMaxRunes = 8000

type FilterScoreInput struct {
	UserEmail   string
	Source      string
	Room        string
	SourceTS    string
	MessageRef  string
	Provider    string
	PActionable float64
	Threshold   float64
	Verdict     string
	Category    string
	Text        string
}

func filterSince(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func InsertFilterScore(ctx context.Context, in FilterScoreInput) (int64, error) {
	conn := GetDB()
	if conn == nil {
		return 0, fmt.Errorf("insert filter score: db not initialized")
	}
	id, err := db.New(conn).InsertFilterScore(ctx, db.InsertFilterScoreParams{
		UserEmail:   in.UserEmail,
		Source:      in.Source,
		Room:        in.Room,
		SourceTs:    in.SourceTS,
		MessageRef:  in.MessageRef,
		Provider:    in.Provider,
		PActionable: in.PActionable,
		Threshold:   in.Threshold,
		Verdict:     in.Verdict,
		Category:    in.Category,
		Text:        truncateRunesExact(in.Text, FilterScoreTextMaxRunes),
	})
	if err != nil {
		return 0, fmt.Errorf("insert filter score: %w", err)
	}
	return id, nil
}

func GetFilterScore(ctx context.Context, email string, id int64) (db.FilterScore, error) {
	row, err := db.New(GetDB()).GetFilterScore(ctx, db.GetFilterScoreParams{ID: id, UserEmail: email})
	if err != nil {
		return db.FilterScore{}, fmt.Errorf("get filter score: %w", err)
	}
	return row, nil
}

func ListFilterDrops(ctx context.Context, email string, since time.Time, limit int) ([]db.FilterScore, error) {
	rows, err := db.New(GetDB()).ListFilterDrops(ctx, db.ListFilterDropsParams{
		UserEmail: email, CreatedAt: filterSince(since), Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list filter drops: %w", err)
	}
	return rows, nil
}

func ListBorderlineDrops(ctx context.Context, email string, since time.Time, lo, hi float64, limit int) ([]db.FilterScore, error) {
	rows, err := db.New(GetDB()).ListBorderlineDrops(ctx, db.ListBorderlineDropsParams{
		UserEmail: email, Since: filterSince(since), Lo: lo, Hi: hi, RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list borderline drops: %w", err)
	}
	return rows, nil
}

func SetFilterOutcome(ctx context.Context, email string, id int64, outcome string) error {
	err := db.New(GetDB()).SetFilterOutcome(ctx, db.SetFilterOutcomeParams{Outcome: outcome, ID: id, UserEmail: email})
	if err != nil {
		return fmt.Errorf("set filter outcome: %w", err)
	}
	return nil
}

func LinkFilterOutcomeBySourceTS(ctx context.Context, email, source, room, sourceTS, outcome string) error {
	err := db.New(GetDB()).LinkFilterOutcomeBySourceTS(ctx, db.LinkFilterOutcomeBySourceTSParams{
		Outcome: outcome, UserEmail: email, Source: source, Room: room, SourceTs: sourceTS,
	})
	if err != nil {
		return fmt.Errorf("link filter outcome: %w", err)
	}
	return nil
}

func ListFilterScoresSince(ctx context.Context, email string, since time.Time) ([]db.FilterScore, error) {
	rows, err := db.New(GetDB()).ListFilterScoresSince(ctx, db.ListFilterScoresSinceParams{
		UserEmail: email, CreatedAt: filterSince(since),
	})
	if err != nil {
		return nil, fmt.Errorf("list filter scores: %w", err)
	}
	return rows, nil
}

func ListFilterUsers(ctx context.Context, since time.Time) ([]string, error) {
	users, err := db.New(GetDB()).ListFilterUsers(ctx, filterSince(since))
	if err != nil {
		return nil, fmt.Errorf("list filter users: %w", err)
	}
	return users, nil
}

func DeleteFilterScoresBefore(ctx context.Context, cutoff time.Time) error {
	if err := db.New(GetDB()).DeleteFilterScoresBefore(ctx, filterSince(cutoff)); err != nil {
		return fmt.Errorf("delete filter scores: %w", err)
	}
	return nil
}

func truncateRunesExact(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
