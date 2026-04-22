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
	
	// Why: Perform one-time migration of legacy aliases if the new table is empty from migrations.go.

	// Why: DSU is handled in-memory but persisted merge relations must be loaded.
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
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	mappings, ok := contactsCache[email]
	if !ok {
		return []ContactRecord{}, nil
	}

	result := make([]ContactRecord, len(mappings))
	copy(result, mappings)
	return result, nil
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

	UpdateContactsCache(id, tenantEmail, canonicalID, displayName, source)
	return id, nil
}

// searchCache is moved or merged into specific find helpers.

// UpdateContactsCache localizes cache update logic for reuse across manual and automatic upserts.
func UpdateContactsCache(id int64, email, canonicalID, displayName, source string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	
	if _, ok := contactsCache[email]; !ok {
		contactsCache[email] = []ContactRecord{}
	}
	idx := slices.IndexFunc(contactsCache[email], func(m ContactRecord) bool {
		return m.CanonicalID == canonicalID
	})
	if idx >= 0 {
		contactsCache[email][idx].ID = id
		contactsCache[email][idx].DisplayName = displayName
		contactsCache[email][idx].Source = source
	} else {
		contactsCache[email] = append(contactsCache[email], ContactRecord{
			ID:          id,
			TenantEmail: email,
			CanonicalID: canonicalID,
			DisplayName: displayName,
			Source:      source,
			ContactType: CategoryNone, // Default for manual cache update
		})
	}
}

// AutoUpsertContact provides a safe, automatic way to register new email contacts found during ingestion.
// It merges new aliases and avoids overwriting meaningful display names with raw email addresses.
func AutoUpsertContact(ctx context.Context, tenantEmail, email, name, source string) error {
	canonicalID := strings.ToLower(strings.TrimSpace(email))
	if canonicalID == "" {
		return nil
	}

	metadataMu.RLock()
	existing := findInCache(tenantEmail, func(m ContactRecord) bool {
		return m.CanonicalID == canonicalID
	})
	metadataMu.RUnlock()

	newName := strings.TrimSpace(name)
	isValidName := newName != "" && !strings.Contains(newName, "@") && strings.ToLower(newName) != canonicalID

	// If we have nothing to improve, skip the DB write entirely.
	if existing != nil && !isValidName {
		return nil
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

	metadataMu.RLock()
	// Number already stored as secondary_id on an email contact — no further action
	linkedToEmail := findInCache(email, func(m ContactRecord) bool {
		return m.CanonicalID != number && slices.Contains(m.SecondaryIDs, number)
	})
	// Existing canonical WA contact for this number
	waContact := findInCache(email, func(m ContactRecord) bool {
		return m.CanonicalID == number
	})
	// Email contact whose display name matches — candidate for secondary_id linking
	emailContact := findInCache(email, func(m ContactRecord) bool {
		return m.CanonicalID != number && strings.EqualFold(m.DisplayName, name)
	})
	metadataMu.RUnlock()

	if linkedToEmail != nil {
		return nil
	}
	// If a matching email contact exists and no canonical WA contact yet, link as secondary_id
	if emailContact != nil && emailContact.ID > 0 && waContact == nil {
		return appendSecondaryID(ctx, email, emailContact.ID, number)
	}
	// Update existing WA contact's display name if it's a human-readable name
	if waContact != nil {
		if waContact.DisplayName == number || waContact.DisplayName == "" || strings.Contains(waContact.DisplayName, " ") {
			_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
			return err
		}
		return nil
	}

	_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
	return err
}

func appendSecondaryID(ctx context.Context, tenantEmail string, contactID int64, value string) error {
	err := db.New(GetDB()).AppendSecondaryID(ctx, db.AppendSecondaryIDParams{
		Value: value,
		ID:    contactID,
	})
	if err != nil {
		return err
	}
	metadataMu.Lock()
	for i := range contactsCache[tenantEmail] {
		if contactsCache[tenantEmail][i].ID == contactID {
			contactsCache[tenantEmail][i].SecondaryIDs = append(contactsCache[tenantEmail][i].SecondaryIDs, value)
			break
		}
	}
	metadataMu.Unlock()
	return nil
}

func GetNameByWhatsAppNumber(email, number string) string {
	id, err := ResolveAlias(context.Background(), ContactTypeWhatsApp, number)
	if err != nil {
		return ""
	}

	metadataMu.RLock()
	found := findInCache(email, func(m ContactRecord) bool {
		return m.ID == id
	})
	metadataMu.RUnlock()
	if found != nil {
		return found.DisplayName
	}
	return ""
}

func NormalizeContactName(email, rawName string) string {
	if rawName == "" {
		return ""
	}

	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()

	// Why: Lazily populate the cache if it was invalidated or not yet loaded for this tenant.
	if !ok && GetDB() != nil {
		all, _ := fetchAllTenantContacts(context.Background(), email)
		if all != nil {
			metadataMu.Lock()
			contactsCache[email] = all
			mappings = all
			metadataMu.Unlock()
		}
	}

	// Helper for matching
	match := func(records []ContactRecord, raw string) string {
		// 1. Precise Identity-X resolution
		id, err := ResolveAlias(context.Background(), ContactTypeName, raw)
		if err == nil {
			for i := range records {
				if records[i].ID == id {
					return records[i].DisplayName
				}
			}
		}
		// 2. Fuzzy/Identifier matching
		normalized := strings.TrimSpace(strings.ToLower(raw))
		for i := range records {
			if strings.ToLower(records[i].DisplayName) == normalized || strings.ToLower(records[i].CanonicalID) == normalized {
				return records[i].DisplayName
			}
		}
		return ""
	}

	if name := match(mappings, rawName); name != "" {
		return name
	}

	// Why: If not found in cache, it might be due to a recent partial cache invalidation (e.g. during promotion loop).
	// We force a reload from DB once more before giving up.
	all, err := fetchAllTenantContacts(context.Background(), email)
	if err == nil {
		metadataMu.Lock()
		contactsCache[email] = all
		metadataMu.Unlock()
		if name := match(all, rawName); name != "" {
			return name
		}
	}

	return rawName
}


// GetContactsByIdentifiers fetches multiple contacts in a single pass to eliminate N+1 overhead in report generation.
func GetContactsByIdentifiers(ctx context.Context, tenantEmail string, identifiers []string) (map[string]*ContactRecord, map[string]bool, error) {
	if len(identifiers) == 0 {
		return make(map[string]*ContactRecord), make(map[string]bool), nil
	}

	res := make(map[string]*ContactRecord)
	ambiguous := make(map[string]bool)
	var remaining []string

	metadataMu.RLock()
	mappings := contactsCache[tenantEmail]
	for _, id := range identifiers {
		normalized := NormalizeIdentifier(id)
		if id == tenantEmail {
			res[id] = &ContactRecord{ID: 0, TenantEmail: tenantEmail, CanonicalID: tenantEmail, DisplayName: "Me", ContactType: CategoryInternal}
			continue
		}
		matches := findAllInMappings(mappings, normalized)
		if len(matches) > 1 {
			ambiguous[id] = true
			res[id] = matches[0]
		} else if len(matches) == 1 {
			res[id] = matches[0]
		} else {
			remaining = append(remaining, id)
		}
	}
	metadataMu.RUnlock()

	if len(remaining) == 0 {
		return res, ambiguous, nil
	}

	return fetchContactsBatch(ctx, tenantEmail, remaining, res, ambiguous)
}

func findAllInMappings(mappings []ContactRecord, normalized string) []*ContactRecord {
	var res []*ContactRecord
	seen := make(map[string]bool)
	for i := range mappings {
		m := &mappings[i]
		if matchContact(m, normalized) {
			if m.ID == -1 {
				return nil
			}
			dedupeKey := m.CanonicalID
			if m.MasterContactID.Valid {
				dedupeKey = fmt.Sprintf("master:%d", m.MasterContactID.Int64)
			}
			if !seen[dedupeKey] {
				entry := m
				if m.MasterContactID.Valid {
					if master := findByID(mappings, m.MasterContactID.Int64); master != nil {
						entry = master
					}
				}
				res = append(res, entry)
				seen[dedupeKey] = true
			}
		}
	}
	return res
}


func fetchContactsBatch(ctx context.Context, tenantEmail string, remaining []string, res map[string]*ContactRecord, ambiguous map[string]bool) (map[string]*ContactRecord, map[string]bool, error) {
	all, _ := fetchAllTenantContacts(ctx, tenantEmail)
	
	// Phase 1: Bulk resolve unknown identifiers using Identity-X
	resolvedIDs, ambigMap := BulkResolveIdentityX(ctx, remaining)
	
	// Phase 2: Map resolved IDs back to ContactRecords and handle missing/ambiguous
	for _, id := range remaining {
		normalized := NormalizeIdentifier(id)
		
		// Case A: Identity-X identified this as ambiguous
		if ambigMap[normalized] {
			ambiguous[id] = true
			res[id] = &ContactRecord{ID: -1} // Sentinel for ambiguous
			continue
		}
		
		// Case B: Identity-X found a single mapping
		if foundID, ok := resolvedIDs[normalized]; ok {
			if contact := findByID(all, foundID); contact != nil {
				res[id] = contact
				continue
			}
		}
		
		// Case C: Truly missing - negative cache it
		recordMissing(tenantEmail, id)
	}
	
	return res, ambiguous, nil
}

// BulkResolveIdentityX performs a batch lookup of identifiers in contact_aliases with SQLite-safe chunking.
// Why: Eliminates N+1 performance bottleneck by consolidating multiple identity lookups into a single IN query.
func BulkResolveIdentityX(ctx context.Context, identifiers []string) (map[string]int64, map[string]bool) {
	uniqueNormalized := make(map[string]bool)
	var normalizedList []string
	for _, id := range identifiers {
		if norm := NormalizeIdentifier(id); norm != "" && !uniqueNormalized[norm] {
			uniqueNormalized[norm] = true
			normalizedList = append(normalizedList, norm)
		}
	}

	res := make(map[string]int64)
	ambiguous := make(map[string]bool)
	if len(normalizedList) == 0 {
		return res, ambiguous
	}

	// SQLite has a parameter limit (usually 999). We use 500 for safety.
	const chunkSize = 500
	for i := 0; i < len(normalizedList); i += chunkSize {
		end := i + chunkSize
		if end > len(normalizedList) {
			end = len(normalizedList)
		}
		processResolutionChunk(ctx, normalizedList[i:end], res, ambiguous)
	}
	return res, ambiguous
}

func processResolutionChunk(ctx context.Context, chunk []string, res map[string]int64, ambiguous map[string]bool) {
	queries := db.New(GetDB())
	rows, err := queries.GetContactsByValues(ctx, chunk)
	if err != nil {
		return
	}

	chunkSet := make(map[string]bool, len(chunk))
	for _, v := range chunk {
		chunkSet[strings.ToLower(v)] = true
	}

	addMatch := func(m map[string][]int64, val string, id int64) {
		if !slices.Contains(m[val], id) {
			m[val] = append(m[val], id)
		}
	}

	idToContacts := make(map[string][]int64)
	for _, row := range rows {
		canonID := GlobalContactDSU.Find(row.ID)
		if lower := strings.ToLower(row.CanonicalID); chunkSet[lower] {
			addMatch(idToContacts, lower, canonID)
		}
		if dn := strings.ToLower(row.DisplayName); dn != "" && chunkSet[dn] {
			addMatch(idToContacts, dn, canonID)
		}
		for _, sid := range row.SecondaryIDs {
			if sl := strings.ToLower(sid); chunkSet[sl] {
				addMatch(idToContacts, sl, canonID)
			}
		}
	}

	for val, cids := range idToContacts {
		if len(cids) > 1 {
			ambiguous[val] = true
		} else if len(cids) == 1 {
			res[val] = cids[0]
		}
	}
}


func fetchAllTenantContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	queries := db.New(GetDB())
	rows, err := queries.GetContactsByTenant(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}

	all := make([]ContactRecord, len(rows))
	for i, r := range rows {
		var sids []string
		if r.SecondaryIDs != "" && r.SecondaryIDs != "null" {
			_ = json.Unmarshal([]byte(r.SecondaryIDs), &sids)
		}
		all[i] = ContactRecord{
			ID:              r.ID,
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
			SecondaryIDs:    sids,
		}
	}
	return all, nil
}


func findByID(all []ContactRecord, id int64) *ContactRecord {
	for i := range all {
		if all[i].ID == id { return &all[i] }
	}
	return nil
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

func recordMissing(tenantEmail, normalized string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	contactsCache[tenantEmail] = append(contactsCache[tenantEmail], ContactRecord{
		ID: -1, 
		TenantEmail: tenantEmail, 
		CanonicalID: normalized, 
		DisplayName: normalized,
	})
}

func matchContact(c *ContactRecord, normalized string) bool {
	if strings.ToLower(c.CanonicalID) == normalized || strings.ToLower(c.DisplayName) == normalized {
		return true
	}
	for _, sid := range c.SecondaryIDs {
		if strings.ToLower(sid) == normalized {
			return true
		}
	}
	return false
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
	err := db.New(GetDB()).DeleteContactMapping(ctx, db.DeleteContactMappingParams{
		TenantEmail: email,
		CanonicalID: canonicalID,
	})
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		if mappings, ok := contactsCache[email]; ok {
			contactsCache[email] = slices.DeleteFunc(mappings, func(m ContactRecord) bool {
				return m.CanonicalID == canonicalID
			})
		}
	}
	return err
}

func GetContactByID(ctx context.Context, tenantEmail string, id int64) (*ContactRecord, error) {
	row, err := db.New(GetDB()).GetContactByID(ctx, db.GetContactByIDParams{
		TenantEmail: tenantEmail,
		ID:          id,
	})
	if err != nil {
		return nil, err
	}
	var sids []string
	if row.SecondaryIDs != "" && row.SecondaryIDs != "null" {
		_ = json.Unmarshal([]byte(row.SecondaryIDs), &sids)
	}
	return &ContactRecord{
		ID:              row.ID,
		TenantEmail:     row.TenantEmail,
		CanonicalID:     row.CanonicalID,
		DisplayName:     row.DisplayName,
		Source:          row.Source.String,
		MasterContactID: row.MasterContactID,
		ContactType:     row.ContactType.String,
		SecondaryIDs:    sids,
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

	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Safety Check & Type Fetch: If Master is already a Child, use its parent to keep flat tree.
	var masterType string
	var masterParentID *int64
	err = tx.QueryRowContext(ctx, "SELECT master_contact_id, contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(masterID), tenantEmail).Scan(&masterParentID, &masterType)
	if err != nil {
		return err
	}
	if masterParentID != nil {
		masterID = *masterParentID
		if masterID == targetID {
			return fmt.Errorf("circular reference detected: target is already the master of this account")
		}
		_ = tx.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(masterID), tenantEmail).Scan(&masterType)
	}

	// 2. Fetch Target Type
	var targetType string
	err = tx.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(targetID), tenantEmail).Scan(&targetType)
	if err != nil {
		return err
	}

	// 3. Link and Promote Category
	q := db.New(tx)
	if err := q.UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		MasterContactID: nullInt64(int64(masterID)),
		TenantEmail:     tenantEmail,
		ID:              int64(targetID),
	}); err != nil {
		return err
	}
	
	finalType := PromoteContactType(masterType, targetType)
	if finalType != masterType {
		if err := q.UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
			ContactType: nullString(finalType),
			TenantEmail: tenantEmail,
			ID:          int64(masterID),
		}); err != nil {
			return err
		}
		trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ID:%d promoted to %s via merge", masterID, finalType), 0, int(masterID))
		invalidateCache()
	}

	// 4. Record Merge History, DSU, and Flatten
	_ = q.InsertMergeHistory(ctx, db.InsertMergeHistoryParams{
		SourceContactID: int64(targetID),
		TargetContactID: int64(masterID),
		Reason:          "Manual Link",
	})
	GlobalContactDSU.Union(int64(masterID), int64(targetID))
	
	// Flatten children: move all children of targetID to masterID
	// Equivalent to: UPDATE contacts SET master_contact_id = masterID WHERE master_contact_id = targetID
	// Since we removed 'FlattenChildren', we can use pure SQL or expose it.
	// Let's use direct SQL for this specific maintenance task to avoid bloat in contacts.sql.
	if _, err := tx.ExecContext(ctx, "UPDATE contacts SET master_contact_id = ? WHERE master_contact_id = ? AND tenant_email = ?", masterID, targetID, tenantEmail); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Simple cache invalidation
	metadataMu.Lock()
	delete(contactsCache, tenantEmail)
	metadataMu.Unlock()

	return nil
}

func UnlinkContact(ctx context.Context, tenantEmail string, targetID int64) error {
	err := db.New(GetDB()).UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		MasterContactID: sql.NullInt64{Valid: false}, // Set NULL
		TenantEmail:     tenantEmail,
		ID:              int64(targetID),
	})
	if err == nil {
		metadataMu.Lock()
		delete(contactsCache, tenantEmail)
		metadataMu.Unlock()
	}
	return err
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
	conn := GetDB()

	var query string
	var args []interface{}
	switch idType {
	case ContactTypeWhatsApp:
		query = "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?" +
			" UNION SELECT contacts.id FROM contacts, json_each(secondary_ids) j WHERE LOWER(j.value) = ?"
		args = []interface{}{trimmed, trimmed}
	case ContactTypeEmail:
		query = "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?"
		args = []interface{}{trimmed}
	default:
		query = "SELECT id FROM contacts WHERE LOWER(canonical_id) = ? OR LOWER(display_name) = ?"
		args = []interface{}{trimmed, trimmed}
	}

	rows, err := conn.QueryContext(ctx, query, args...)
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

	seen := make(map[int64]bool)
	var canonicalIDs []int64
	for _, rid := range rawIDs {
		cid := GlobalContactDSU.Find(rid)
		if !seen[cid] {
			canonicalIDs = append(canonicalIDs, cid)
			seen[cid] = true
		}
	}

	elapsed := time.Since(start).Milliseconds()
	trace.Step(ctx, "IdentityResolution", fmt.Sprintf("Type: %s, Latency: %dms, Results: %d", idType, elapsed, len(canonicalIDs)), int(elapsed), 0)

	return canonicalIDs, nil
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

// UpdateContactType updates the categorization for a contact and synchronizes cache.
// Why: Standardizes the promotion logic and ensures strict type validation.
func UpdateContactType(ctx context.Context, contactID int64, cType string) error {
	validTypes := []string{CategoryInternal, CategoryPartner, CategoryCustomer, CategoryNone}
	if !slices.Contains(validTypes, cType) {
		return fmt.Errorf("invalid contact type: %s", cType)
	}

	id := int64(contactID)
	// Why: UpdateContactDetails requires tenant_email. We need to fetch it or pass it.
	// For simplicity in this promotion path, we'll fetch it from DB first as this is a low-frequency maintenance task.
	tenantEmail, _ := fetchTenantEmailByID(ctx, id)

	if err := db.New(GetDB()).UpdateContactDetails(ctx, db.UpdateContactDetailsParams{
		ContactType: sql.NullString{String: cType, Valid: cType != ""},
		TenantEmail: tenantEmail,
		ID:          id,
	}); err != nil {
		return err
	}

	trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ContactID:%d set to %s", id, cType), 0, int(id))
	invalidateCache()
	return nil
}

func invalidateCache() {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	for tenant := range contactsCache {
		delete(contactsCache, tenant)
	}
}

func fetchTenantEmailByID(ctx context.Context, id int64) (string, error) {
	var email string
	err := GetDB().QueryRowContext(ctx, "SELECT tenant_email FROM contacts WHERE id = ?", id).Scan(&email)
	return email, err
}

func findInCache(email string, predicate func(ContactRecord) bool) *ContactRecord {
	mappings, ok := contactsCache[email]
	if !ok {
		return nil
	}
	for i := range mappings {
		if predicate(mappings[i]) {
			return &mappings[i]
		}
	}
	return nil
}
