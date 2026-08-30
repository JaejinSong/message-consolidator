package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strings"
	"sync"
)

// autoUpsertCache memoizes (tenantEmail, canonicalID) → displayName from the last
// successful AutoUpsertContact call so that repeat scans of the same Gmail boundary
// messages skip the contacts/contact_resolution SQL chain entirely. A no-op scan
// cycle previously emitted ~5 SQL per recurring sender (INSERT contacts + SELECT
// tenant_email + UPDATE contacts + 2x INSERT contact_resolution) for senders the
// scanner had already registered. The cache resets on process restart; the first
// cycle after restart still re-upserts (UpsertContactMapping uses ON CONFLICT DO
// UPDATE so it is idempotent at the DB level) and warms the cache.
var (
	autoUpsertCacheMu sync.RWMutex
	autoUpsertCache   = make(map[string]string)
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
	logger.Infof("[RESOLUTION] DSU initialized with persistent merge relations")
}

func GetContactsMappings(ctx context.Context, email string) ([]ContactRecord, error) {
	return fetchAllTenantContacts(ctx, email)
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

	displayName := canonicalID
	if isValidName {
		displayName = newName
	}

	cacheKey := tenantEmail + "|" + canonicalID
	autoUpsertCacheMu.RLock()
	cachedName, cacheHit := autoUpsertCache[cacheKey]
	autoUpsertCacheMu.RUnlock()
	if cacheHit && cachedName == displayName {
		return nil
	}

	if !isValidName {
		rows, _ := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{
			TenantEmail: tenantEmail,
			Identifiers: []string{NormalizeIdentifier(canonicalID)},
		})
		if len(rows) > 0 {
			rememberAutoUpsert(cacheKey, displayName)
			return nil
		}
	}

	_, err := UpsertContact(ctx, tenantEmail, canonicalID, displayName, newName, source)
	if err == nil {
		rememberAutoUpsert(cacheKey, displayName)
	}
	return err
}

func rememberAutoUpsert(cacheKey, displayName string) {
	autoUpsertCacheMu.Lock()
	autoUpsertCache[cacheKey] = displayName
	autoUpsertCacheMu.Unlock()
}

func NormalizeContactName(ctx context.Context, email, rawName string) string {
	if rawName == "" || GetDB() == nil {
		return rawName
	}
	norm := NormalizeIdentifier(rawName)
	rows, err := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{
		TenantEmail: email,
		Identifiers: []string{norm},
	})
	if err != nil || len(rows) == 0 {
		return rawName
	}
	byID := fetchContactsByIDs(ctx, []int64{rows[0].ContactID})
	if c, ok := byID[rows[0].ContactID]; ok && c.DisplayName != "" {
		return c.DisplayName
	}
	return rawName
}


// ContactNameKnown reports whether contacts actually resolve rawName for this tenant.
// Why: NormalizeContactName echoes unknown names back unchanged, so callers that
// need an existence check (extraction guard grounding) cannot use it.
func ContactNameKnown(ctx context.Context, email, rawName string) bool {
	if rawName == "" || GetDB() == nil {
		return false
	}
	rows, err := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{
		TenantEmail: email,
		Identifiers: []string{NormalizeIdentifier(rawName)},
	})
	return err == nil && len(rows) > 0
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

func DeleteContactMapping(ctx context.Context, email, canonicalID string) error {
	// contact_resolution entries are removed via ON DELETE CASCADE on the contacts table.
	return db.New(GetDB()).DeleteContactMapping(ctx, db.DeleteContactMappingParams{
		TenantEmail: email,
		CanonicalID: canonicalID,
	})
}
