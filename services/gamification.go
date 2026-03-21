package services

import (
	"message-consolidator/store"
	"time"
)

// ProcessTaskCompletion handles XP, Points, and Streak logic when a task is finished.
func ProcessTaskCompletion(u *store.User) error {
	// 1. Give XP and Points
	newXP := u.XP + 10
	newPoints := u.Points + 5

	// 2. Handle Level Up
	newLevel := (newXP / 100) + 1

	// 3. Handle Streak
	now := time.Now()
	newStreak := u.Streak

	if u.LastCompletedAt == nil {
		newStreak = 1
	} else {
		last := *u.LastCompletedAt
		// Safe date truncation (prevents UTC epoch offset bugs)
		lastDate := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, last.Location())
		nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		daysDiff := int(nowDate.Sub(lastDate).Hours() / 24)

		if daysDiff == 1 {
			newStreak++
		} else if daysDiff > 1 {
			newStreak = 1
		}
	}

	// Update the user's gamification data in the store
	return store.UpdateUserGamification(u.Email, newPoints, newStreak, newLevel, newXP, u.DailyGoal, &now)
}
