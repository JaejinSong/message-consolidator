package store

import (
	"context"
	"message-consolidator/db"
	"strings"
)

func SaveWhatsAppContact(ctx context.Context, email, number, name string) error {
	if number == "" || name == "" || name == number {
		return nil
	}
	queries := db.New(GetDB())
	norm := NormalizeIdentifier(number)
	rows, _ := queries.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: email, Identifiers: []string{norm}})
	if len(rows) > 0 {
		return handleExistingChannelContact(ctx, email, rows[0].ContactID, number, name, ContactTypeWhatsApp)
	}
	nameNorm := NormalizeIdentifier(name)
	nameRows, _ := queries.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: email, Identifiers: []string{nameNorm}})
	if len(nameRows) > 0 {
		return handleWANameMatch(ctx, email, nameRows[0].ContactID, number)
	}
	_, err := UpsertContact(ctx, email, number, name, "", ContactTypeWhatsApp)
	return err
}

func handleExistingChannelContact(ctx context.Context, email string, cid int64, externalID, name string, ct string) error {
	byID := fetchContactsByIDs(ctx, []int64{cid})
	contact, ok := byID[cid]
	if !ok {
		_, err := UpsertContact(ctx, email, externalID, name, "", ct)
		return err
	}
	if contact.CanonicalID != externalID {
		return nil
	}
	if contact.DisplayName == externalID || contact.DisplayName == "" || strings.Contains(name, " ") {
		_, err := UpsertContact(ctx, email, externalID, name, "", ct)
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

// getNameByExternalID resolves a channel-external id to its contact display name,
// scoped to tenantEmail. Why: buildAliasQuery carries no tenant_email predicate, so the
// resolved row is re-fetched tenant-scoped -- the same fail-closed pattern
// resolveContactIdentity uses. Without that second check, a number labelled only by
// another tenant returned THAT tenant's private contact name, which then landed in this
// tenant's message text, participant names and extracted requester/assignee fields.
func getNameByExternalID(ctx context.Context, tenantEmail, ct, externalID string) string {
	id, err := ResolveAlias(ctx, ct, externalID)
	if err != nil {
		return ""
	}
	row, err := db.New(GetDB()).GetContactByID(ctx, db.GetContactByIDParams{
		TenantEmail: tenantEmail,
		ID:          id,
	})
	if err != nil {
		return ""
	}
	return row.DisplayName
}

func GetNameByWhatsAppNumber(ctx context.Context, email, number string) string {
	return getNameByExternalID(ctx, email, ContactTypeWhatsApp, number)
}

// SaveTelegramContact upserts a Telegram user mapping (canonical_id=numeric user ID).
// Mirrors SaveWhatsAppContact: preserves richer display names, merges by identifier when possible.
func SaveTelegramContact(ctx context.Context, email, userID, name string) error {
	if userID == "" || name == "" || name == userID {
		return nil
	}
	queries := db.New(GetDB())
	norm := NormalizeIdentifier(userID)
	rows, _ := queries.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: email, Identifiers: []string{norm}})
	if len(rows) > 0 {
		return handleExistingChannelContact(ctx, email, rows[0].ContactID, userID, name, ContactTypeTelegram)
	}
	_, err := UpsertContact(ctx, email, userID, name, "", ContactTypeTelegram)
	return err
}

func GetNameByTelegramID(ctx context.Context, email, userID string) string {
	return getNameByExternalID(ctx, email, ContactTypeTelegram, userID)
}
