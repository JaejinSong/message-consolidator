package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"message-consolidator/db"
)

// ExclusionCandidate is a confirm-first signal that a task has been unprocessed long
// enough to park it out of tracking. Stored under metadata.exclusion_candidate and
// surfaced in the UI for one-tap confirmation — it never auto-excludes the task.
type ExclusionCandidate struct {
	ProposedAt  string `json:"proposed_at"`
	DaysStalled int    `json:"days_stalled"`
	Status      string `json:"status"` // "pending" | "confirmed"
}

// ExclusionScanRow is an open TASK row past the long-term-unprocessed threshold.
type ExclusionScanRow struct {
	ID          MessageID
	UserEmail   string
	Task        string
	Metadata    string
	UpdatedAt   time.Time
	DaysStalled int
}

// ExcludedItem is a parked task consumed by the periodic digest.
type ExcludedItem struct {
	ID         MessageID
	UserEmail  string
	Task       string
	Room       string
	Source     string
	Link       string
	Metadata   string
	ExcludedAt time.Time
}

// SelectExclusionScan returns open TASK rows (all users) unchanged for thresholdDays.
func SelectExclusionScan(ctx context.Context, thresholdDays int) ([]ExclusionScanRow, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -thresholdDays)
	rows, err := db.New(GetDB()).SelectExclusionCandidateScan(ctx, sql.NullTime{Time: cutoff, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("select exclusion scan: %w", err)
	}
	out := make([]ExclusionScanRow, 0, len(rows))
	for _, r := range rows {
		row := ExclusionScanRow{
			ID:          MessageID(r.ID),
			UserEmail:   r.UserEmail,
			Task:        r.Task,
			Metadata:    r.Metadata,
			DaysStalled: int(r.DaysStalled),
		}
		if r.UpdatedAt.Valid {
			row.UpdatedAt = r.UpdatedAt.Time
		}
		out = append(out, row)
	}
	return out, nil
}

// ProposeExclusionCandidate records a pending exclusion candidate on a task's metadata.
// Why: raw json_set merges into existing metadata without a read-modify-write race,
// and sqlc cannot represent JSON path updates (see AddCompletionCandidate).
func ProposeExclusionCandidate(ctx context.Context, q Querier, email string, id MessageID, cand ExclusionCandidate) error {
	payload, err := json.Marshal(cand)
	if err != nil {
		return fmt.Errorf("marshal exclusion candidate: %w", err)
	}
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages
			SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyExclusionCandidate + `', json(?))
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, string(payload), int64(id), email); err != nil {
			return fmt.Errorf("propose exclusion candidate: %w", err)
		}
		InvalidateCache(email)
		return nil
	})
}

// DismissExclusionCandidate removes a pending exclusion candidate and stamps the
// dismissal time so the scan does not re-propose until the repropose window elapses.
func DismissExclusionCandidate(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages SET metadata = json_set(
				json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyExclusionCandidate + `'),
				'$.` + metaKeyExclusionDismissedAt + `', ?
			)
			WHERE id = ? AND user_email = ?`
		dismissedAt := time.Now().UTC().Format(time.RFC3339)
		if _, err := qw.ExecContext(ctx, stmt, dismissedAt, int64(id), email); err != nil {
			return fmt.Errorf("dismiss exclusion candidate: %w", err)
		}
		InvalidateCache(email)
		return nil
	})
}

// ConfirmExclusion parks the task (excluded_at=now) and marks the candidate confirmed.
// Returns sql.ErrNoRows when the task is already done/deleted so callers can 404.
func ConfirmExclusion(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		affected, err := db.New(qw).ConfirmExclusion(ctx, db.ConfirmExclusionParams{
			ID:        int64(id),
			UserEmail: nullString(email),
		})
		if err != nil {
			return fmt.Errorf("confirm exclusion: %w", err)
		}
		if affected == 0 {
			return sql.ErrNoRows
		}
		const stmt = `UPDATE messages
			SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyExclusionCandidate + `.status', 'confirmed')
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, int64(id), email); err != nil {
			return fmt.Errorf("mark exclusion confirmed: %w", err)
		}
		InvalidateCache(email)
		return nil
	})
}

// RestoreExcluded moves a parked task back to active tracking and clears every
// exclusion marker so the task gets a fresh long-term-unprocessed runway.
func RestoreExcluded(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		affected, err := db.New(qw).RestoreExcluded(ctx, db.RestoreExcludedParams{
			ID:        int64(id),
			UserEmail: nullString(email),
		})
		if err != nil {
			return fmt.Errorf("restore excluded: %w", err)
		}
		if affected == 0 {
			return sql.ErrNoRows
		}
		const stmt = `UPDATE messages SET metadata = json_remove(
				COALESCE(NULLIF(metadata, ''), '{}'),
				'$.` + metaKeyExclusionCandidate + `',
				'$.` + metaKeyExclusionDismissedAt + `',
				'$.` + metaKeyRemindedPrefix + `excluded_digest'
			)
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, int64(id), email); err != nil {
			return fmt.Errorf("clear exclusion markers: %w", err)
		}
		InvalidateCache(email)
		return nil
	})
}

// AutoRestoreIfExcluded un-parks a task when new inbound activity lands on it.
// Why: exclusion means "parked for inactivity" — fresh activity invalidates the premise,
// and restoring is the fail-open direction (worst case: one-tap re-exclusion next cycle).
// No-op (false, nil) when the task is not excluded. Safe inside a caller's transaction.
func AutoRestoreIfExcluded(ctx context.Context, q Querier, email string, id MessageID) (bool, error) {
	const stmt = `UPDATE messages
		SET excluded_at = NULL,
		    metadata = json_set(
		        json_remove(COALESCE(NULLIF(metadata, ''), '{}'),
		            '$.` + metaKeyExclusionCandidate + `',
		            '$.` + metaKeyExclusionDismissedAt + `',
		            '$.` + metaKeyRemindedPrefix + `excluded_digest'),
		        '$.` + metaKeyExcludedAutoRestoredAt + `', ?)
		WHERE id = ? AND user_email = ? AND excluded_at IS NOT NULL`
	res, err := q.ExecContext(ctx, stmt, time.Now().UTC().Format(time.RFC3339), int64(id), email)
	if err != nil {
		return false, fmt.Errorf("auto-restore excluded: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("auto-restore rows affected: %w", err)
	}
	if affected == 0 {
		return false, nil
	}
	InvalidateCache(email)
	return true, nil
}

// SelectExcludedDigestItems returns all parked tasks across users for the digest loop.
func SelectExcludedDigestItems(ctx context.Context) ([]ExcludedItem, error) {
	rows, err := db.New(GetDB()).SelectExcludedForDigest(ctx)
	if err != nil {
		return nil, fmt.Errorf("select excluded for digest: %w", err)
	}
	out := make([]ExcludedItem, 0, len(rows))
	for _, r := range rows {
		item := ExcludedItem{
			ID:        MessageID(r.ID),
			UserEmail: r.UserEmail,
			Task:      r.Task,
			Room:      r.Room,
			Source:    r.Source,
			Link:      r.Link,
			Metadata:  r.Metadata,
		}
		if r.ExcludedAt.Valid {
			item.ExcludedAt = r.ExcludedAt.Time
		}
		out = append(out, item)
	}
	return out, nil
}

// HasExclusionCandidate reports whether metadata already carries an exclusion candidate.
func HasExclusionCandidate(metadata string) bool {
	return ParseMetadata(metadata).Has(metaKeyExclusionCandidate)
}

// HasPendingCompletionCandidate reports whether a confirm-first completion suggestion
// is pending — completion evidence outranks abandonment, so the exclusion scan skips.
func HasPendingCompletionCandidate(metadata string) bool {
	var cand CompletionCandidate
	if !ParseMetadata(metadata).Decode(metaKeyCompletionCandidate, &cand) {
		return false
	}
	return cand.Status == "pending"
}

// ExclusionDismissedAt returns the dismissal timestamp, if any.
func ExclusionDismissedAt(metadata string) (time.Time, bool) {
	return ParseMetadata(metadata).Time(metaKeyExclusionDismissedAt)
}

// RemindedWithin reports whether reminded_at_<window> was stamped within d of now.
// Why: periodic digests overwrite the same key; recency (not existence) gates re-sends.
func RemindedWithin(metadata, window string, d time.Duration) bool {
	t, ok := ParseMetadata(metadata).Time(metaKeyReminded(window))
	return ok && time.Since(t) < d
}
