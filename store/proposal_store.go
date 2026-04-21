package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"message-consolidator/db"
	"sort"
)

// ProposalGroup is a pending identity merge proposal returned to the UI.
type ProposalGroup struct {
	GroupID    string          `json:"group_id"`
	Contacts   []ContactRecord `json:"contacts"`
	Confidence float64         `json:"confidence"`
	Reason     string          `json:"reason"`
}

// NewGroupID generates a random 16-char hex group identifier.
func NewGroupID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetStandaloneContacts returns all unlinked (master_contact_id IS NULL) contacts for a tenant.
// These are the candidates the AI scans when generating merge proposals.
func GetStandaloneContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	rows, err := GetDB().QueryContext(ctx,
		`SELECT id, tenant_email, canonical_id, display_name,
		        COALESCE(source, ''), master_contact_id, COALESCE(contact_type, 'none')
		 FROM contacts
		 WHERE tenant_email = ? AND master_contact_id IS NULL`,
		tenantEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContactRecord
	for rows.Next() {
		var c ContactRecord
		var masterID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &masterID, &c.ContactType); err != nil {
			return nil, err
		}
		c.MasterContactID = masterID
		result = append(result, c)
	}
	return result, rows.Err()
}

// LoadHandledPairs returns all contact pairs already recorded for the tenant (any status).
// The map key is [2]int64{smaller_id, larger_id} to ensure canonical ordering.
func LoadHandledPairs(ctx context.Context, tenantEmail string) (map[[2]int64]bool, error) {
	rows, err := db.New(GetDB()).ListAllPairs(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}
	pairs := make(map[[2]int64]bool, len(rows))
	for _, r := range rows {
		pairs[r] = true // ListAllPairs already stores with contact_id_a < contact_id_b
	}
	return pairs, nil
}

// InsertProposalGroup stores all contact pairs for a group under a shared proposal_group_id.
func InsertProposalGroup(ctx context.Context, groupID string, contactIDs []int64, confidence float64, reason string) error {
	q := db.New(GetDB())
	for i := 0; i < len(contactIDs); i++ {
		for j := i + 1; j < len(contactIDs); j++ {
			a, b := contactIDs[i], contactIDs[j]
			if a > b {
				a, b = b, a
			}
			if err := q.InsertMergeProposal(ctx, a, b, confidence, reason, groupID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListPendingProposalGroups returns all pending proposals grouped for UI display.
// All contacts are batch-loaded in a single call to avoid N+1 queries.
func ListPendingProposalGroups(ctx context.Context, tenantEmail string) ([]ProposalGroup, error) {
	rows, err := db.New(GetDB()).ListProposalGroupRows(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}

	type entry struct {
		contactIDs map[int64]bool
		confidence float64
		reason     string
	}
	groupMap := map[string]*entry{}
	allContactIDs := make(map[int64]bool)

	for _, r := range rows {
		g, ok := groupMap[r.GroupID]
		if !ok {
			g = &entry{contactIDs: make(map[int64]bool)}
			groupMap[r.GroupID] = g
		}
		g.contactIDs[r.ContactIDA] = true
		g.contactIDs[r.ContactIDB] = true
		allContactIDs[r.ContactIDA] = true
		allContactIDs[r.ContactIDB] = true
		if r.Confidence > g.confidence {
			g.confidence = r.Confidence
		}
		if r.Reason.Valid && g.reason == "" {
			g.reason = r.Reason.String
		}
	}

	// Batch-load all contacts at once instead of one call per group.
	allIDs := make([]int64, 0, len(allContactIDs))
	for id := range allContactIDs {
		allIDs = append(allIDs, id)
	}
	allContacts, err := getContactsByIDs(ctx, tenantEmail, allIDs)
	if err != nil {
		return nil, err
	}
	contactByID := make(map[int64]ContactRecord, len(allContacts))
	for _, c := range allContacts {
		contactByID[c.ID] = c
	}

	var result []ProposalGroup
	for gid, g := range groupMap {
		contacts := make([]ContactRecord, 0, len(g.contactIDs))
		for id := range g.contactIDs {
			if c, ok := contactByID[id]; ok {
				contacts = append(contacts, c)
			}
		}
		result = append(result, ProposalGroup{
			GroupID:    gid,
			Contacts:   contacts,
			Confidence: g.confidence,
			Reason:     g.reason,
		})
	}
	return result, nil
}

// AcceptProposalGroup links all contacts in the group under a single master and sets the canonical display name.
func AcceptProposalGroup(ctx context.Context, tenantEmail, groupID, canonicalName string) error {
	rows, err := db.New(GetDB()).ListProposalGroupRowsByID(ctx, groupID, tenantEmail)
	if err != nil {
		return err
	}

	contactIDs := make(map[int64]bool)
	for _, r := range rows {
		contactIDs[r.ContactIDA] = true
		contactIDs[r.ContactIDB] = true
	}
	if len(contactIDs) == 0 {
		return fmt.Errorf("proposal group %s not found", groupID)
	}

	ids := make([]int64, 0, len(contactIDs))
	for id := range contactIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	masterID := ids[0]
	for _, targetID := range ids[1:] {
		if err := LinkContact(ctx, tenantEmail, masterID, targetID); err != nil {
			return err
		}
	}

	if canonicalName != "" {
		if _, err := GetDB().ExecContext(ctx,
			`UPDATE contacts SET display_name = ? WHERE id = ? AND tenant_email = ?`,
			canonicalName, masterID, tenantEmail,
		); err != nil {
			return err
		}
		invalidateCache()
	}

	return db.New(GetDB()).UpdateProposalGroupStatus(ctx, groupID, "accepted", canonicalName)
}

// RejectProposalGroup marks all rows in the group as rejected.
func RejectProposalGroup(ctx context.Context, tenantEmail, groupID string) error {
	return db.New(GetDB()).UpdateProposalGroupStatus(ctx, groupID, "rejected", "")
}

// getContactsByIDs loads contacts by ID, using the in-memory cache with DB fallback.
func getContactsByIDs(ctx context.Context, tenantEmail string, ids []int64) ([]ContactRecord, error) {
	idSet := make(map[int64]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	metadataMu.RLock()
	var result []ContactRecord
	for _, c := range contactsCache[tenantEmail] {
		if idSet[c.ID] {
			result = append(result, c)
			delete(idSet, c.ID)
		}
	}
	metadataMu.RUnlock()

	// DB fallback for any not found in cache
	for id := range idSet {
		var c ContactRecord
		var masterID sql.NullInt64
		err := GetDB().QueryRowContext(ctx,
			`SELECT id, tenant_email, canonical_id, display_name,
			        COALESCE(source, ''), master_contact_id, COALESCE(contact_type, 'none')
			 FROM contacts WHERE id = ? AND tenant_email = ?`,
			id, tenantEmail,
		).Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &masterID, &c.ContactType)
		if err == nil {
			c.MasterContactID = masterID
			result = append(result, c)
		}
	}
	return result, nil
}
