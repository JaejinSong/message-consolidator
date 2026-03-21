package services

import (
	"message-consolidator/store"
)

// HandleTaskCompletion orchestrates the process of marking a task as done, 
// updating gamification stats, and potentially recording statistics for analytics.
func HandleTaskCompletion(email string, taskID int, done bool) error {
	// 1. Mark the message as done in the database
	if err := store.MarkMessageDone(email, taskID, done); err != nil {
		return err
	}

	// 2. If the task was completed (done == true), update gamification stats
	if done {
		user, err := store.GetOrCreateUser(email, "", "")
		if err != nil {
			return err
		}

		// Process XP, Level, and Streak
		if err := ProcessTaskCompletion(user); err != nil {
			// Log error but don't fail the whole request (soft-fail)
			// (Assuming we might have a logger available here or pass it in)
		}
	}

	return nil
}
