package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"slices"

	"github.com/whatap/go-api/trace"
)

// RebuildContactResolution rebuilds the contact_resolution table for a tenant from all existing contacts.
func RebuildContactResolution(ctx context.Context, tenantEmail string) error {
	all, err := fetchAllTenantContacts(ctx, tenantEmail)
	if err != nil {
		return err
	}
	for _, c := range all {
		rootID := GlobalContactDSU.Find(c.ID)
		upsertResolutionForContact(ctx, tenantEmail, rootID, c.CanonicalID, c.DisplayName, c.SecondaryIDs)
	}
	return nil
}

func upsertResolutionForContact(ctx context.Context, tenantEmail string, contactID int64, canonicalID, displayName string, secondaryIDs []string) {
	queries := db.New(GetDB())
	seen := make(map[string]bool)
	upsert := func(raw string) {
		norm := NormalizeIdentifier(raw)
		if norm == "" || seen[norm] {
			return
		}
		seen[norm] = true
		_ = queries.UpsertContactResolution(ctx, db.UpsertContactResolutionParams{
			TenantEmail:   tenantEmail,
			RawIdentifier: norm,
			ContactID:     contactID,
		})
	}
	upsert(canonicalID)
	if displayName != canonicalID {
		upsert(displayName)
	}
	for _, sid := range secondaryIDs {
		upsert(sid)
	}
}

func fetchContactsByIDs(ctx context.Context, ids []int64) map[int64]*ContactRecord {
	if len(ids) == 0 {
		return make(map[int64]*ContactRecord)
	}
	rows, err := db.New(GetDB()).GetContactsByIDs(ctx, ids)
	if err != nil {
		return make(map[int64]*ContactRecord)
	}
	result := make(map[int64]*ContactRecord, len(rows))
	for _, r := range rows {
		c := &ContactRecord{
			ID:              r.ID,
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
			SecondaryIDs:    parseSecondaryIDs(r.SecondaryIds),
		}
		result[r.ID] = c
	}
	return result
}

func fetchAllTenantContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	queries := db.New(GetDB())
	rows, err := queries.GetContactsByTenant(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}

	all := make([]ContactRecord, len(rows))
	for i, r := range rows {
		all[i] = ContactRecord{
			ID:              r.ID,
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
			SecondaryIDs:    parseSecondaryIDs(r.SecondaryIds),
		}
	}
	return all, nil
}

func LinkContact(ctx context.Context, tenantEmail string, masterID, targetID int64) error {
	if masterID == targetID {
		return fmt.Errorf("cannot link a contact to itself")
	}
	tx, err := GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	masterID, masterType, err := resolveEffectiveMaster(ctx, tx, tenantEmail, masterID, targetID)
	if err != nil {
		return err
	}
	q := db.New(tx)
	targetRow, err := q.GetContactTypeByID(ctx, db.GetContactTypeByIDParams{ID: targetID, TenantEmail: tenantEmail})
	if err != nil {
		return err
	}
	targetType := targetRow.String
	if err := applyLinkUpdates(ctx, tx, tenantEmail, masterID, targetID, masterType, targetType); err != nil {
		return err
	}
	if err := q.FlattenContactChildren(ctx, db.FlattenContactChildrenParams{MasterContactID: sql.NullInt64{Int64: masterID, Valid: true}, MasterContactID_2: sql.NullInt64{Int64: targetID, Valid: true}, TenantEmail: tenantEmail}); err != nil {
		return err
	}
	if slaveName, err := q.GetDisplayNameByID(ctx, targetID); err == nil && slaveName != "" {
		_ = q.UpdateDisplayNameIfEmpty(ctx, db.UpdateDisplayNameIfEmptyParams{DisplayName: slaveName, ID: masterID, TenantEmail: tenantEmail})
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = db.New(GetDB()).UpdateResolutionContactID(ctx, db.UpdateResolutionContactIDParams{
		ContactID:   masterID,
		TenantEmail: tenantEmail,
		ContactID_2: targetID,
	})
	return nil
}

func resolveEffectiveMaster(ctx context.Context, tx *sql.Tx, tenantEmail string, masterID, targetID int64) (int64, string, error) {
	q := db.New(tx)
	row, err := q.GetMasterAndTypeByID(ctx, db.GetMasterAndTypeByIDParams{ID: masterID, TenantEmail: tenantEmail})
	if err != nil {
		return 0, "", err
	}
	if !row.MasterContactID.Valid {
		return masterID, row.ContactType.String, nil
	}
	masterID = row.MasterContactID.Int64
	if masterID == targetID {
		return 0, "", fmt.Errorf("circular reference detected: target is already the master of this account")
	}
	parentRow, _ := q.GetMasterAndTypeByID(ctx, db.GetMasterAndTypeByIDParams{ID: masterID, TenantEmail: tenantEmail})
	return masterID, parentRow.ContactType.String, nil
}

func applyLinkUpdates(ctx context.Context, tx *sql.Tx, tenantEmail string, masterID, targetID int64, masterType, targetType string) error {
	q := db.New(tx)
	if err := q.UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		MasterContactID: nullInt64(masterID),
		TenantEmail:     tenantEmail,
		ID:              targetID,
	}); err != nil {
		return err
	}
	finalType := PromoteContactType(masterType, targetType)
	if finalType != masterType {
		if err := q.UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
			ContactType: nullString(finalType),
			TenantEmail: tenantEmail,
			ID:          masterID,
		}); err != nil {
			return err
		}
		_ = trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ID:%d promoted to %s via merge", masterID, finalType), 0, int(masterID))
	}
	_ = q.InsertMergeHistory(ctx, db.InsertMergeHistoryParams{
		SourceContactID: targetID,
		TargetContactID: masterID,
		Reason:          "Manual Link",
	})
	GlobalContactDSU.Union(masterID, targetID)
	return nil
}

func UnlinkContact(ctx context.Context, tenantEmail string, targetID int64) error {
	err := db.New(GetDB()).UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		MasterContactID: sql.NullInt64{Valid: false},
		TenantEmail:     tenantEmail,
		ID:              int64(targetID),
	})
	if err != nil {
		return err
	}
	// Rebuild resolution table for this tenant to reflect the unlink.
	return RebuildContactResolution(ctx, tenantEmail)
}

func GetLinkedContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	rows, err := db.New(GetDB()).GetLinkedContacts(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}
	var results []ContactRecord
	for _, r := range rows {
		results = append(results, ContactRecord{
			ID:              int64(r.ID),
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
		})
	}
	return results, nil
}

// PromoteContactType returns the higher ranking category between two types.
// Rank: internal(4) > partner(3) > customer(2) > none(1)
func PromoteContactType(current, newcomer string) string {
	ranks := map[string]int{
		CategoryInternal: 4,
		CategoryPartner:  3,
		CategoryCustomer: 2,
		CategoryNone:     1,
	}
	if ranks[newcomer] > ranks[current] {
		return newcomer
	}
	return current
}

// UpdateContactType updates the categorization for a contact.
func UpdateContactType(ctx context.Context, contactID int64, cType string) error {
	validTypes := []string{CategoryInternal, CategoryPartner, CategoryCustomer, CategoryNone}
	if !slices.Contains(validTypes, cType) {
		return fmt.Errorf("invalid contact type: %s", cType)
	}

	id := int64(contactID)
	tenantEmail, _ := fetchTenantEmailByID(ctx, id)

	if err := db.New(GetDB()).UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		ContactType: sql.NullString{String: cType, Valid: cType != ""},
		TenantEmail: tenantEmail,
		ID:          id,
	}); err != nil {
		return err
	}

	_ = trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ContactID:%d set to %s", id, cType), 0, int(id))
	return nil
}

func fetchTenantEmailByID(ctx context.Context, id int64) (string, error) {
	return db.New(GetDB()).GetTenantEmailByContactID(ctx, id)
}
