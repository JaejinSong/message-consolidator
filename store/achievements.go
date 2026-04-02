package store

import (
	"message-consolidator/logger"
)

func setupGamification() error {
	if _, err := db.Exec(SQL.CreateAchievementsTable); err != nil {
		logger.Errorf("[DB-INIT] Failed to create achievements: %v", err)
	}
	if _, err := db.Exec(SQL.CreateUserAchievementsTable); err != nil {
		logger.Errorf("[DB-INIT] Failed to create user_achievements: %v", err)
	}

	seedAchievements()
	InitContactsTable()
	InitTokenUsageTable()
	return nil
}

func seedAchievements() {
	var count int
	err := db.QueryRow(SQL.GetAchievementCount).Scan(&count)
	if err != nil {
		logger.Errorf("[ACHIEVEMENTS] Failed to count existing achievements: %v", err)
		return
	}

	//Why: Seeds initial achievement data if the current count is below established baseline thresholds.
	if count < 5 {
		//Why: Resets achievements to ensure the latest definitions are applied during the seeding process.
		_, err = db.Exec(SQL.DeleteAllAchievements)
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to clear achievements for seeding: %v", err)
		}
		
		_, err = db.Exec(SQL.SeedAchievements)
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to seed achievements: %v", err)
		}
	}
}
