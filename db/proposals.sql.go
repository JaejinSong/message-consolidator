package db

import (
	"context"
	"database/sql"
	"time"
)

// ProposalGroupRow is a single row from identity_merge_candidates that belongs to a named group.
type ProposalGroupRow struct {
	GroupID    string
	ContactIDA int64
	ContactIDB int64
	Confidence float64
	Reason     sql.NullString
	Status     string
	CreatedAt  time.Time
}

// InsertMergeProposal inserts a contact pair into identity_merge_candidates under a shared group ID.
// INSERT OR IGNORE preserves the UNIQUE(contact_id_a, contact_id_b) constraint idempotently.
func (q *Queries) InsertMergeProposal(ctx context.Context, contactIDA, contactIDB int64, confidence float64, reason, groupID string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO identity_merge_candidates
		 (contact_id_a, contact_id_b, confidence, reason, proposal_group_id, status)
		 VALUES (?, ?, ?, ?, ?, 'pending')`,
		contactIDA, contactIDB, confidence, reason, groupID,
	)
	return err
}

// ListProposalGroupRows returns all pending rows that belong to a proposal group for the given tenant.
func (q *Queries) ListProposalGroupRows(ctx context.Context, tenantEmail string) ([]ProposalGroupRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT imc.proposal_group_id, imc.contact_id_a, imc.contact_id_b,
		        imc.confidence, imc.reason, imc.status, imc.created_at
		 FROM identity_merge_candidates imc
		 JOIN contacts ca ON imc.contact_id_a = ca.id AND ca.tenant_email = ?
		 WHERE imc.proposal_group_id IS NOT NULL AND imc.status = 'pending'
		 ORDER BY imc.created_at DESC`,
		tenantEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProposalGroupRow
	for rows.Next() {
		var r ProposalGroupRow
		if err := rows.Scan(&r.GroupID, &r.ContactIDA, &r.ContactIDB, &r.Confidence, &r.Reason, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ListAllPairs returns all contact pairs ever recorded for the tenant, regardless of status.
// Used to skip AI proposals where every pair has already been handled.
func (q *Queries) ListAllPairs(ctx context.Context, tenantEmail string) ([][2]int64, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT imc.contact_id_a, imc.contact_id_b
		 FROM identity_merge_candidates imc
		 JOIN contacts ca ON imc.contact_id_a = ca.id AND ca.tenant_email = ?
		 WHERE imc.proposal_group_id IS NOT NULL`,
		tenantEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result [][2]int64
	for rows.Next() {
		var a, b int64
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		result = append(result, [2]int64{a, b})
	}
	return result, rows.Err()
}

// ListProposalGroupRowsByID returns pending rows for a single proposal group, scoped to the tenant.
func (q *Queries) ListProposalGroupRowsByID(ctx context.Context, groupID, tenantEmail string) ([]ProposalGroupRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT imc.proposal_group_id, imc.contact_id_a, imc.contact_id_b,
		        imc.confidence, imc.reason, imc.status, imc.created_at
		 FROM identity_merge_candidates imc
		 JOIN contacts ca ON imc.contact_id_a = ca.id AND ca.tenant_email = ?
		 WHERE imc.proposal_group_id = ? AND imc.status = 'pending'`,
		tenantEmail, groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProposalGroupRow
	for rows.Next() {
		var r ProposalGroupRow
		if err := rows.Scan(&r.GroupID, &r.ContactIDA, &r.ContactIDB, &r.Confidence, &r.Reason, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// UpdateProposalGroupStatus sets status and canonical_name for every row sharing a group ID.
func (q *Queries) UpdateProposalGroupStatus(ctx context.Context, groupID, status, canonicalName string) error {
	_, err := q.db.ExecContext(ctx,
		`UPDATE identity_merge_candidates SET status = ?, canonical_name = ? WHERE proposal_group_id = ?`,
		status, canonicalName, groupID,
	)
	return err
}
