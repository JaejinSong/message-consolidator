package store

import (
	"message-consolidator/logger"
)

func setupGamification(q Querier) error {
	// Tables are now created in createCoreTables before migrations.
	seedAchievements(q)
	InitContactsTable(q)
	InitTokenUsageTable(q)
	return nil
}

func seedAchievements(q Querier) {
	var count int
	err := q.QueryRow(SQL.GetAchievementCount).Scan(&count)
	if err != nil {
		logger.Errorf("[ACHIEVEMENTS] Failed to count existing achievements: %v", err)
		return
	}

	//Why: Seeds initial achievement data if the current count is below established baseline thresholds.
	if count < 5 {
		//Why: Resets achievements to ensure the latest definitions are applied during the seeding process.
		_, err = q.Exec(SQL.DeleteAllAchievements)
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to clear achievements for seeding: %v", err)
		}
		
		_, err = q.Exec(SQL.SeedAchievements)
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to seed achievements: %v", err)
		}
	}
}
