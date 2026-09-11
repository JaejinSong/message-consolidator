package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"message-consolidator/db"
	"message-consolidator/logger"
	"message-consolidator/store"
)

// The precision observer turns the user's own triage into rule proposals.
//
// Why a new observation kind: correction_observations already drives suppression, but its
// suppress arm keys on a sorted token bag of one message, so two different messages never
// share a key -- in production all 49 observations sat at evidence_count=1 with none
// promoted while 726 cancellations went unused. Cancellations do not repeat textually;
// they repeat along dimensions (who owns the task, which room, which leading verb).
//
// Why these proposals can never be applied: ListActiveSuppressRules filters
// kind = 'suppress'. kind = 'precision' is outside that filter, so guardSuppressRule
// cannot reach these rows whatever their status. That is a structural guarantee rather
// than a convention, and it matters because an auto-applied suppression is unfalsifiable:
// once the engine stops extracting a bucket, no further evidence about that bucket ever
// arrives. These rows exist to be read and approved by a person.
const (
	// precisionWindowDays bounds measurement to recent behaviour, so a rule earned months
	// ago does not outlive the habit that produced it.
	precisionWindowDays = 61
	// precisionMinResolved is the floor on decided tasks in a bucket. Below it the rate is
	// noise: at 3 tasks a single cancellation reads as 33%.
	precisionMinResolved = 7
	// precisionMinCancelRate sits well above the ~40% baseline so a qualifying bucket is
	// clearly worse than the system average rather than merely average.
	precisionMinCancelRate = 0.7
	// precisionMaxSampleIDs caps the stored examples; they exist so a reviewer can check
	// the proposal against real rows, not to mirror the table.
	precisionMaxSampleIDs = 11
)

// precisionBucket is one measured slice of the user's triage.
type precisionBucket struct {
	key      string // stored as from_value, e.g. "owner=shared" or "verb=review"
	scope    string // stored as scope, e.g. "whatsapp|Adira - Whatap Tech"
	resolved int64
	canceled int64
	ids      []string
}

func (b precisionBucket) cancelRate() float64 {
	if b.resolved == 0 {
		return 0
	}
	return float64(b.canceled) / float64(b.resolved)
}

func (b precisionBucket) qualifies() bool {
	return b.resolved >= precisionMinResolved && b.cancelRate() >= precisionMinCancelRate
}

// ObserveAllPrecision proposes rules for every user. A failure for one user must not stop
// the others, so errors are logged per user and the sweep continues.
func ObserveAllPrecision(ctx context.Context) error {
	users, err := store.GetAllUsers(ctx)
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		n, err := ObservePrecision(ctx, u.Email)
		if err != nil {
			logger.Warnf("[PRECISION] observe %s failed: %v", u.Email, err)
			continue
		}
		if n > 0 {
			logger.Infof("[PRECISION] %s: %d bucket(s) proposed", u.Email, n)
		}
	}
	return nil
}

// ObservePrecision measures the user's buckets and records the ones worth a human's
// attention. Returns how many qualified.
func ObservePrecision(ctx context.Context, email string) (int, error) {
	conn := store.GetDB()
	if conn == nil {
		return 0, nil
	}
	q := db.New(conn)
	since := time.Now().AddDate(0, 0, -precisionWindowDays)

	buckets, err := collectPrecisionBuckets(ctx, q, email, since)
	if err != nil {
		return 0, err
	}

	proposed := 0
	for _, b := range buckets {
		if !b.qualifies() {
			continue
		}
		ids := b.ids
		if len(ids) > precisionMaxSampleIDs {
			ids = ids[:precisionMaxSampleIDs]
		}
		sample, err := json.Marshal(ids)
		if err != nil {
			sample = []byte("[]")
		}
		if err := q.UpsertPrecisionObservation(ctx, db.UpsertPrecisionObservationParams{
			UserEmail:      email,
			FromValue:      b.key,
			Scope:          b.scope,
			EvidenceCount:  b.canceled,
			SeenMessageIds: string(sample),
		}); err != nil {
			logger.Warnf("[PRECISION] upsert %s %s failed: %v", b.scope, b.key, err)
			continue
		}
		proposed++
	}
	return proposed, nil
}

func collectPrecisionBuckets(ctx context.Context, q *db.Queries, email string, since time.Time) ([]precisionBucket, error) {
	userArg := sql.NullString{String: email, Valid: true}
	sinceArg := sql.NullTime{Time: since, Valid: true}

	var out []precisionBucket

	owners, err := q.PrecisionBucketsByOwner(ctx, db.PrecisionBucketsByOwnerParams{
		UserEmail: userArg, CreatedAt: sinceArg,
	})
	if err != nil {
		return nil, fmt.Errorf("owner buckets: %w", err)
	}
	for _, r := range owners {
		// Why both classes: filtering to shared looked prudent but discarded the strongest
		// signal in the room that produced the user's own false positive -- Internal
		// Puspakom WhaTap IFC cancels 85% of unowned tasks and 78.9% of owned ones, so the
		// room is the problem there, not the ownership. A reviewer needs to see that.
		out = append(out, precisionBucket{
			key:      "owner=" + r.OwnerClass,
			scope:    r.Source + "|" + r.Room,
			resolved: r.Resolved,
			canceled: nullFloatToInt(r.Canceled),
			ids:      splitConcatIDs(r.CanceledIds),
		})
	}

	verbs, err := q.PrecisionBucketsByVerb(ctx, db.PrecisionBucketsByVerbParams{
		UserEmail: userArg, CreatedAt: sinceArg,
	})
	if err != nil {
		return nil, fmt.Errorf("verb buckets: %w", err)
	}
	for _, r := range verbs {
		verb := strings.TrimSpace(r.Verb)
		if verb == "" {
			continue
		}
		out = append(out, precisionBucket{
			key:      "verb=" + verb,
			scope:    r.Source,
			resolved: r.Resolved,
			canceled: nullFloatToInt(r.Canceled),
			ids:      splitConcatIDs(r.CanceledIds),
		})
	}
	return out, nil
}

// nullFloatToInt narrows SUM()'s float result. Why: sqlc types an aggregate SUM as
// NullFloat64 even when every term is an integer.
func nullFloatToInt(v sql.NullFloat64) int64 {
	if !v.Valid {
		return 0
	}
	return int64(v.Float64)
}

// splitConcatIDs reads group_concat output, which sqlc types as interface{} and which is
// empty rather than NULL when the bucket has no cancellations.
func splitConcatIDs(v any) []string {
	raw := ""
	switch t := v.(type) {
	case string:
		raw = t
	case []byte:
		raw = string(t)
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
