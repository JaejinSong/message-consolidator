package store

import (
	"context"
	"testing"
	"message-consolidator/internal/testutil"

	"github.com/stretchr/testify/assert"
)

func TestContactTypePromotion(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := "test@example.com"

	t.Run("Promotion via Email Alias", func(t *testing.T) {
		// 1. Create a 'none' contact
		id, err := AddContact(ctx, tenant, "external@gmail.com", "External User", "", "gmail")
		assert.NoError(t, err)

		c, _ := GetContactByID(ctx, tenant, id)
		if c != nil {
			assert.Equal(t, "none", c.ContactType)
		}

		// 2. Register internal email alias
		err = RegisterAlias(ctx, id, "email", "staff@whatap.io", "manual", 5)
		assert.NoError(t, err)

		// 3. Verify promotion to 'internal'
		c, _ = GetContactByID(ctx, tenant, id)
		if c != nil {
			assert.Equal(t, "internal", c.ContactType)
		}
	})

	t.Run("Promotion via Merge Hierarchy", func(t *testing.T) {
		// internal(4) > partner(3) > customer(2) > none(1)
		
		// 1. Create Customer
		custID, _ := AddContact(ctx, tenant, "cust@client.com", "Customer", "", "crm")
		err := UpdateContactType(ctx, custID, "customer")
		assert.NoError(t, err)

		// 2. Create Partner
		partID, _ := AddContact(ctx, tenant, "part@vendor.com", "Partner", "", "crm")
		err = UpdateContactType(ctx, partID, "partner")
		assert.NoError(t, err)

		// 3. Merge Partner (Master) and Customer (Target)
		err = LinkContact(ctx, tenant, partID, custID)
		assert.NoError(t, err)

		// 4. Verify Master remains Partner
		master, _ := GetContactByID(ctx, tenant, partID)
		if master != nil {
			assert.Equal(t, "partner", master.ContactType)
		}

		// 5. Create Internal
		intID, _ := AddContact(ctx, tenant, "boss@company.com", "Boss", "", "hr")
		err = UpdateContactType(ctx, intID, "internal")
		assert.NoError(t, err)

		// 6. Merge Customer (Master) and Internal (Target)
		// Note: LinkContact uses parent if masterID is already a child, 
		// but here custID is already under partID.
		err = LinkContact(ctx, tenant, custID, intID)
		assert.NoError(t, err)

		// The final master (partID) should be promoted to 'internal' 
		// because one of the merging parties (intID) was internal.
		finalMaster, _ := GetContactByID(ctx, tenant, partID)
		if finalMaster != nil {
			assert.Equal(t, "internal", finalMaster.ContactType)
		}
	})

	t.Run("Invalid Type Validation", func(t *testing.T) {
		id, _ := AddContact(ctx, tenant, "test@test.com", "Tester", "", "test")
		err := UpdateContactType(ctx, id, "invalid_category")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid contact type")
	})

	t.Run("PromoteContactType Utility", func(t *testing.T) {
		assert.Equal(t, "internal", PromoteContactType("customer", "internal"))
		assert.Equal(t, "partner", PromoteContactType("partner", "customer"))
		assert.Equal(t, "customer", PromoteContactType("none", "customer"))
		assert.Equal(t, "partner", PromoteContactType("partner", "none"))
	})
}
