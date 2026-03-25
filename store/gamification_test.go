package store

import (
	"testing"
)

func TestAchievementConsistency(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	// 1. 시딩 확인: 최소 9종의 업적이 등록되어 있어야 함
	achievements, err := GetAchievements()
	if err != nil {
		t.Fatalf("Failed to get achievements: %v", err)
	}

	if len(achievements) < 9 {
		t.Errorf("Expected at least 9 achievements, got %d", len(achievements))
	}

	// 2. 특정 업적 존재 확인
	foundMorningStar := false
	for _, a := range achievements {
		if a.Name == "모닝 스타" {
			foundMorningStar = true
			if a.CriteriaType != "early_bird" || a.CriteriaValue != 1 {
				t.Errorf("Invalid criteria for Morning Star: %s=%d", a.CriteriaType, a.CriteriaValue)
			}
		}
	}
	if !foundMorningStar {
		t.Error("Morning Star achievement not found in seeded data")
	}
}

func TestRetroactiveAchievementUnlock(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	t.Run("Check Logic Integration", func(t *testing.T) {
		// 테스트용 사용자 생성
		_, err := GetOrCreateUser("test_ach@example.com", "Test Ach User", "")
		if err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
		
		var u User
		_ = db.QueryRow(SQL.GetUserByEmail, "test_ach@example.com").Scan(
			&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture,
			&u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal,
			&u.LastCompletedAt, &u.CreatedAt, &u.StreakFreezes,
		)

		// GetUserAchievements를 호출하여 리팩토링된 로직이 에러 없이 작동하는지 확인
		_, err = GetUserAchievements(u.ID)
		if err != nil {
			t.Errorf("GetUserAchievements failed: %v", err)
		}
	})
}
