package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

//Why: Verifies that the initial achievement data is correctly seeded into the database during initialization.
func TestAchievementSeeding(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	achievements, err := GetAchievements(context.Background())
	if err != nil {
		t.Fatalf("Failed to get all achievements during test setup: %v", err)
	}

	t.Run("should seed a minimum number of achievements", func(t *testing.T) {
		const minExpectedAchievements = 9
		if len(achievements) < minExpectedAchievements {
			t.Errorf("Expected at least %d achievements to be seeded, but found %d", minExpectedAchievements, len(achievements))
		}
	})

	//Why: Confirms that the critical 'Morning Star' achievement exists with correct metadata for the early bird trigger logic.
	t.Run("should contain a valid 'Morning Star' achievement", func(t *testing.T) {
		var morningStarAchievement *Achievement
		for i := range achievements {
			if achievements[i].Name == "Morning Star" {
				morningStarAchievement = &achievements[i]
				break
			}
		}

		if morningStarAchievement == nil {
			t.Fatal("'Morning Star' achievement not found in seeded data")
		}

		// Why: These criteria are fundamental to the 'early_bird' trigger logic.
		// If they are changed, the corresponding service logic might fail silently.
		expectedType := "early_bird"
		expectedValue := 1
		if morningStarAchievement.CriteriaType != expectedType || morningStarAchievement.CriteriaValue != expectedValue {
			t.Errorf("Invalid criteria for 'Morning Star'. got type=%s, value=%d; want type=%s, value=%d",
				morningStarAchievement.CriteriaType, morningStarAchievement.CriteriaValue, expectedType, expectedValue)
		}
	})
}

//Why: Serves as a basic smoke test for the achievement retrieval logic, ensuring it handles clean user slates without errors.
// which is a baseline requirement for the retroactive achievement calculation logic it contains.
func TestGetUserAchievements(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	t.Run("should execute without error for a newly created user", func(t *testing.T) {
		// Why: This test ensures that the complex logic inside GetUserAchievements,
		// which might perform calculations or backfills, doesn't fail on a clean user slate.
		user, err := GetOrCreateUser(context.Background(), "test_ach@example.com", "Test Ach User", "")
		if err != nil {
			t.Fatalf("Failed to create a test user: %v", err)
		}

		_, err = GetUserAchievements(context.Background(), user.ID)
		if err != nil {
			t.Errorf("GetUserAchievements failed for a new user: %v", err)
		}
	})
}
