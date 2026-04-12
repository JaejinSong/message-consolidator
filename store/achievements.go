package store

import (
	"context"
	"message-consolidator/db"
	"message-consolidator/logger"
)

func setupGamification(q Querier) error {
	// Tables are now created in createCoreTables before migrations.
	seedAchievements(q)
	return nil
}

func seedAchievements(q Querier) {
	queries := db.New(q)
	count, err := queries.GetAchievementsCount(context.Background())
	if err != nil {
		logger.Errorf("[ACHIEVEMENTS] Failed to count existing achievements: %v", err)
		return
	}

	//Why: Seeds initial achievement data if the current count is 0.
	if count == 0 {
		//Why: Resets achievements to ensure the latest definitions are applied during the seeding process.
		err = queries.DeleteAllAchievements(context.Background())
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to clear achievements for seeding: %v", err)
		}
		
		err = queries.SeedAchievements(context.Background())
		if err != nil {
			logger.Errorf("[ACHIEVEMENTS] Failed to seed achievements: %v", err)
		}
	}
}
