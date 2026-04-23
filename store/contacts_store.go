package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
	"slices"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

var ErrAmbiguousIdentity = errors.New("ambiguous identity match")

func parseSecondaryIDs(raw sql.NullString) []string {
	if raw.String == "" || raw.String == "null" {
		return nil
	}
	var sids []string
	_ = json.Unmarshal([]byte(raw.String), &sids)
	return sids
}

type AmbiguousIdentityError struct {
	Identifier string
	Emails     []string
}

func (e *AmbiguousIdentityError) Error() string {
	return "ambiguous identity match: " + e.Identifier
}

var (
	// GlobalContactDSU maintains the in-memory disjoint-set for fast canonical resolution.
	GlobalContactDSU = NewContactDSU()
)

type ContactRecord struct {
	ID              int64         `json:"id"`
	TenantEmail     string        `json:"tenant_email"`
	CanonicalID     string        `json:"canonical_id"`
	DisplayName     string        `json:"display_name"`
	Source          string        `json:"source"`
	MasterContactID sql.NullInt64 `json:"master_contact_id,omitempty"`
	ContactType     string        `json:"contact_type"`
	SecondaryIDs    []string      `json:"secondary_ids,omitempty"`
}

func InitContactsTable(ctx context.Context, q db.DBTX) {
	queries := db.New(q)
	_ = queries.CreateContactsTable(ctx)
	_ = queries.CreateIdentityMergeHistoryTable(ctx)
	_ = queries.CreateIdentityMergeCandidatesTable(ctx)
	loadDSUFromDB(ctx)
}

func loadDSUFromDB(ctx context.Context) {
	queries := db.New(GetDB())
	rows, err := queries.GetContactsWithMaster(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.MasterContactID.Valid {
			GlobalContactDSU.Union(row.MasterContactID.Int64, row.ID)
		}
	}
	logger.Infof("[Identity-X] DSU initialized with persistent merge relations.")
}

func GetContactsMappings(email string) ([]ContactRecord, error) {
	return fetchAllTenantContacts(context.Background(), email)
}

func AddContactMapping(ctx context.Context, email, canonicalID, displayName, aliases, source string) error {
	_, err := UpsertContact(ctx, email, canonicalID, displayName, aliases, source)
	return err
}

func AddContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	return UpsertContact(ctx, tenantEmail, canonicalID, displayName, aliases, source)
}

func UpsertContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = SourceAll
	}
	id, err := db.New(GetDB()).UpsertContactMapping(ctx, db.UpsertContactMappingParams{
		TenantEmail: tenantEmail,
		CanonicalID: canonicalID,
		DisplayName: displayName,
		Source:      nullString(source),
	})
	if err != nil {
		return 0, err
	}

	if strings.HasSuffix(strings.ToLower(canonicalID), "@whatap.io") {
		_ = UpdateContactType(ctx, id, CategoryInternal)
	}

	rootID := GlobalContactDSU.Find(id)
	upsertResolutionForContact(ctx, tenantEmail, rootID, canonicalID, displayName, nil)
	return id, nil
}

// AutoUpsertContact provides a safe, automatic way to register new email contacts found during ingestion.
func AutoUpsertContact(ctx context.Context, tenantEmail, email, name, source string) error {
	canonicalID := strings.ToLower(strings.TrimSpace(email))
	if canonicalID == "" {
		return nil
	}

	newName := strings.TrimSpace(name)
	isValidName := newName != "" && !strings.Contains(newName, "@") && strings.ToLower(newName) != canonicalID

	if !isValidName {
		rows, _ := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{
			TenantEmail: tenantEmail,
			Identifiers: []string{NormalizeIdentifier(canonicalID)},
		})
		if len(rows) > 0 {
			return nil
		}
	}

	displayName := canonicalID
	if isValidName {
		displayName = newName
	}
	_, err := UpsertContact(ctx, tenantEmail, canonicalID, displayName, newName, source)
	return err
}

func SaveWhatsAppContact(ctx context.Context, email, number, name string) error {
	if number == "" || name == "" || name == number {
		return nil
	}
	queries := db.New(GetDB())
	norm := NormalizeIdentifier(number)
	rows, _ := queries.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: email, Identifiers: []string{norm}})
	if len(rows) > 0 {
		return handleExistingWAContact(ctx, email, rows[0].ContactID, number, name)
	}
	nameNorm := NormalizeIdentifier(name)
	nameRows, _ := queries.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: email, Identifiers: []string{nameNorm}})
	if len(nameRows) > 0 {
		return handleWANameMatch(ctx, email, nameRows[0].ContactID, number)
	}
	_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
	return err
}

func handleExistingWAContact(ctx context.Context, email string, cid int64, number, name string) error {
	byID := fetchContactsByIDs(ctx, []int64{cid})
	contact, ok := byID[cid]
	if !ok {
		_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
		return err
	}
	if contact.CanonicalID != number {
		return nil
	}
	if contact.DisplayName == number || contact.DisplayName == "" || strings.Contains(name, " ") {
		_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
		return err
	}
	return nil
}

func handleWANameMatch(ctx context.Context, email string, cid int64, number string) error {
	byID := fetchContactsByIDs(ctx, []int64{cid})
	emailContact, ok := byID[cid]
	if !ok || emailContact.CanonicalID == number {
		return nil
	}
	return appendSecondaryID(ctx, email, emailContact.ID, number)
}

func appendSecondaryID(ctx context.Context, tenantEmail string, contactID int64, value string) error {
	err := db.New(GetDB()).AppendSecondaryID(ctx, db.AppendSecondaryIDParams{
		JsonInsert: value,
		ID:         contactID,
	})
	if err != nil {
		return err
	}
	norm := NormalizeIdentifier(value)
	if norm != "" {
		rootID := GlobalContactDSU.Find(contactID)
		_ = db.New(GetDB()).UpsertContactResolution(ctx, db.UpsertContactResolutionParams{
			TenantEmail:   tenantEmail,
			RawIdentifier: norm,
			ContactID:     rootID,
		})
	}
	return nil
}

func GetNameByWhatsAppNumber(email, number string) string {
	id, err := ResolveAlias(context.Background(), ContactTypeWhatsApp, number)
	if err != nil {
		return ""
	}
	byID := fetchContactsByIDs(context.Background(), []int64{id})
	if c, ok := byID[id]; ok {
		return c.DisplayName
	}
	return ""
}

func NormalizeContactName(email, rawName string) string {
	if rawName == "" || GetDB() == nil {
		return rawName
	}
	norm := NormalizeIdentifier(rawName)
	rows, err := db.New(GetDB()).GetResolutionsByIdentifiers(context.Background(), db.GetResolutionsByIdentifiersParams{
		TenantEmail: email,
		Identifiers: []string{norm},
	})
	if err != nil || len(rows) == 0 {
		return rawName
	}
	byID := fetchContactsByIDs(context.Background(), []int64{rows[0].ContactID})
	if c, ok := byID[rows[0].ContactID]; ok && c.DisplayName != "" {
		return c.DisplayName
	}
	return rawName
}

// GetContactsByIdentifiers resolves identifiers via the contact_resolution table in a single batch pass.
func GetContactsByIdentifiers(ctx context.Context, tenantEmail string, identifiers []string) (map[string]*ContactRecord, map[string]bool, error) {
	if len(identifiers) == 0 {
		return make(map[string]*ContactRecord), make(map[string]bool), nil
	}
	res := make(map[string]*ContactRecord)
	ambiguous := make(map[string]bool)

	normToOriginals, normList, preResolved := normalizeIdentifierList(tenantEmail, identifiers)
	for k, v := range preResolved {
		res[k] = v
	}
	if len(normList) == 0 {
		return res, ambiguous, nil
	}

	rows, err := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: tenantEmail, Identifiers: normList})
	if err != nil {
		return res, ambiguous, err
	}

	normToContactID := make(map[string]int64, len(rows))
	contactIDSet := make(map[int64]bool)
	for _, row := range rows {
		normToContactID[row.RawIdentifier] = row.ContactID
		contactIDSet[row.ContactID] = true
	}
	idList := make([]int64, 0, len(contactIDSet))
	for id := range contactIDSet {
		idList = append(idList, id)
	}
	contactByID := fetchContactsByIDs(ctx, idList)
	followMasterContacts(ctx, contactByID)

	ambiguousNorms := detectDisplayNameAmbiguity(ctx, tenantEmail, normList)
	for norm, originals := range normToOriginals {
		if ambiguousNorms[norm] {
			for _, orig := range originals {
				ambiguous[orig] = true
			}
			continue
		}
		cid, ok := normToContactID[norm]
		if !ok {
			continue
		}
		for _, orig := range originals {
			res[orig] = contactByID[cid]
		}
	}
	return res, ambiguous, nil
}

func normalizeIdentifierList(tenantEmail string, identifiers []string) (normToOriginals map[string][]string, normList []string, preResolved map[string]*ContactRecord) {
	normToOriginals = make(map[string][]string)
	preResolved = make(map[string]*ContactRecord)
	for _, id := range identifiers {
		if id == tenantEmail {
			preResolved[id] = &ContactRecord{ID: 0, TenantEmail: tenantEmail, CanonicalID: tenantEmail, DisplayName: "Me", ContactType: CategoryInternal}
			continue
		}
		norm := NormalizeIdentifier(id)
		if norm == "" {
			continue
		}
		if _, seen := normToOriginals[norm]; !seen {
			normList = append(normList, norm)
		}
		normToOriginals[norm] = append(normToOriginals[norm], id)
	}
	return
}

func followMasterContacts(ctx context.Context, contactByID map[int64]*ContactRecord) {
	masterIDs := make(map[int64]bool)
	for _, c := range contactByID {
		if c.MasterContactID.Valid {
			masterIDs[c.MasterContactID.Int64] = true
		}
	}
	if len(masterIDs) == 0 {
		return
	}
	masterList := make([]int64, 0, len(masterIDs))
	for mid := range masterIDs {
		masterList = append(masterList, mid)
	}
	masterByID := fetchContactsByIDs(ctx, masterList)
	for id, c := range contactByID {
		if !c.MasterContactID.Valid {
			continue
		}
		master, ok := masterByID[c.MasterContactID.Int64]
		if !ok {
			continue
		}
		if master.DisplayName == "" {
			master.DisplayName = c.DisplayName
		}
		contactByID[id] = master
	}
}

// detectDisplayNameAmbiguity returns normalized display names that are shared by multiple unmerged contacts.
// Uses COALESCE(master_contact_id, id) from DB directly to avoid stale GlobalContactDSU false positives.
func detectDisplayNameAmbiguity(ctx context.Context, tenantEmail string, normList []string) map[string]bool {
	if len(normList) == 0 {
		return nil
	}
	placeholders := strings.Repeat(",?", len(normList))[1:]
	args := make([]interface{}, len(normList)+1)
	args[0] = tenantEmail
	for i, n := range normList {
		args[i+1] = n
	}
	rows, err := GetDB().QueryContext(ctx,
		"SELECT COALESCE(master_contact_id, id), LOWER(display_name) FROM contacts WHERE tenant_email = ? AND LOWER(display_name) IN ("+placeholders+")",
		args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	dnToRoots := make(map[string]map[int64]bool)
	for rows.Next() {
		var root int64
		var dn string
		if err := rows.Scan(&root, &dn); err == nil {
			if dnToRoots[dn] == nil {
				dnToRoots[dn] = make(map[int64]bool)
			}
			dnToRoots[dn][root] = true
		}
	}
	result := make(map[string]bool)
	for dn, roots := range dnToRoots {
		if len(roots) > 1 {
			result[dn] = true
		}
	}
	return result
}

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

// BulkResolveAliases resolves multiple names to their display names in one pass.
func BulkResolveAliases(ctx context.Context, tenantEmail string, names []string) map[string]string {
	if len(names) == 0 {
		return make(map[string]string)
	}
	res, _, err := GetContactsByIdentifiers(ctx, tenantEmail, names)
	if err != nil {
		return fallbackToOriginal(names)
	}
	return buildResolutionMap(names, res)
}

func fallbackToOriginal(names []string) map[string]string {
	m := make(map[string]string)
	for _, n := range names {
		m[n] = n
	}
	return m
}

func buildResolutionMap(names []string, res map[string]*ContactRecord) map[string]string {
	m := make(map[string]string)
	for _, n := range names {
		if c, ok := res[n]; ok && c != nil && c.DisplayName != "" {
			m[n] = c.DisplayName
		} else {
			m[n] = n
		}
	}
	return m
}

// GetContactByIdentifier provides a backward-compatible wrapper for single identity resolution.
func GetContactByIdentifier(ctx context.Context, tenantEmail, identifier string) (*ContactRecord, error) {
	res, ambig, err := GetContactsByIdentifiers(ctx, tenantEmail, []string{identifier})
	if err != nil {
		return nil, err
	}
	if ambig[identifier] {
		return nil, &AmbiguousIdentityError{Identifier: identifier}
	}
	return res[identifier], nil
}

func DeleteContactMapping(ctx context.Context, email, canonicalID string) error {
	// contact_resolution entries are removed via ON DELETE CASCADE on the contacts table.
	return db.New(GetDB()).DeleteContactMapping(ctx, db.DeleteContactMappingParams{
		TenantEmail: email,
		CanonicalID: canonicalID,
	})
}

func GetContactByID(ctx context.Context, tenantEmail string, id int64) (*ContactRecord, error) {
	row, err := db.New(GetDB()).GetContactByID(ctx, db.GetContactByIDParams{
		TenantEmail: tenantEmail,
		ID:          id,
	})
	if err != nil {
		return nil, err
	}
	return &ContactRecord{
		ID:              row.ID,
		TenantEmail:     row.TenantEmail,
		CanonicalID:     row.CanonicalID,
		DisplayName:     row.DisplayName,
		Source:          row.Source.String,
		MasterContactID: row.MasterContactID,
		ContactType:     row.ContactType.String,
		SecondaryIDs:    parseSecondaryIDs(row.SecondaryIds),
	}, nil
}

func SearchContacts(ctx context.Context, tenantEmail, query string) ([]ContactRecord, error) {
	rows, err := db.New(GetDB()).SearchContacts(ctx, db.SearchContactsParams{
		TenantEmail: tenantEmail,
		Column2:     nullString(query),
		Column3:     nullString(query),
	})
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

func LinkContact(ctx context.Context, tenantEmail string, masterID, targetID int64) error {
	if masterID == targetID {
		return fmt.Errorf("cannot link a contact to itself")
	}
	tx, err := GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
		trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ID:%d promoted to %s via merge", masterID, finalType), 0, int(masterID))
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

func ResolveAliases(ctx context.Context, idType, value string) ([]int64, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	start := time.Now()

	query, args := buildAliasQuery(idType, trimmed)
	rows, err := GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rawIDs []int64
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err == nil {
			rawIDs = append(rawIDs, rid)
		}
	}
	if len(rawIDs) == 0 {
		return nil, sql.ErrNoRows
	}

	canonicalIDs := deduplicateByDSU(rawIDs)
	elapsed := time.Since(start).Milliseconds()
	trace.Step(ctx, "IdentityResolution", fmt.Sprintf("Type: %s, Latency: %dms, Results: %d", idType, elapsed, len(canonicalIDs)), int(elapsed), 0)
	return canonicalIDs, nil
}

func buildAliasQuery(idType, trimmed string) (string, []interface{}) {
	switch idType {
	case ContactTypeWhatsApp:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?" +
			" UNION SELECT contacts.id FROM contacts, json_each(secondary_ids) j WHERE LOWER(j.value) = ?",
			[]interface{}{trimmed, trimmed}
	case ContactTypeEmail:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?", []interface{}{trimmed}
	default:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ? OR LOWER(display_name) = ?", []interface{}{trimmed, trimmed}
	}
}

func deduplicateByDSU(rawIDs []int64) []int64 {
	seen := make(map[int64]bool)
	result := make([]int64, 0, len(rawIDs))
	for _, rid := range rawIDs {
		cid := GlobalContactDSU.Find(rid)
		if !seen[cid] {
			result = append(result, cid)
			seen[cid] = true
		}
	}
	return result
}

// ResolveAlias is a convenience wrapper for ResolveAliases when only one result is expected.
func ResolveAlias(ctx context.Context, idType, value string) (int64, error) {
	ids, err := ResolveAliases(ctx, idType, value)
	if err != nil {
		return 0, err
	}
	if len(ids) > 1 {
		return 0, ErrAmbiguousIdentity
	}
	return ids[0], nil
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

	trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ContactID:%d set to %s", id, cType), 0, int(id))
	return nil
}

func fetchTenantEmailByID(ctx context.Context, id int64) (string, error) {
	return db.New(GetDB()).GetTenantEmailByContactID(ctx, id)
}

// ResolvedContact holds the effective contact info after following master_contact_id.
type ResolvedContact struct {
	DisplayName string
	CanonicalID string
	ContactType string
}

// BuildContactResolver loads all contacts for a tenant once and returns a map
// from raw canonical_id → effective resolved contact (merge-aware).
func BuildContactResolver(ctx context.Context, tenantEmail string) (map[string]ResolvedContact, error) {
	contacts, err := fetchAllTenantContacts(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]ContactRecord, len(contacts))
	for _, c := range contacts {
		byID[c.ID] = c
	}
	resolver := make(map[string]ResolvedContact, len(contacts))
	for _, c := range contacts {
		eff := ResolvedContact{DisplayName: c.DisplayName, CanonicalID: c.CanonicalID, ContactType: c.ContactType}
		if c.MasterContactID.Valid {
			if master, ok := byID[c.MasterContactID.Int64]; ok {
				eff.DisplayName = master.DisplayName
				eff.CanonicalID = master.CanonicalID
				eff.ContactType = master.ContactType
			}
		}
		resolver[c.CanonicalID] = eff
	}
	return resolver, nil
}

// resolveContact looks up display name, canonical ID, and type for a raw identifier.
// Falls back to the raw value when not found.
func resolveContact(resolver map[string]ResolvedContact, raw string) (display, canonical, contactType string) {
	if r, ok := resolver[raw]; ok {
		return r.DisplayName, r.CanonicalID, r.ContactType
	}
	return raw, raw, "none"
}
