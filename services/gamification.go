package services

import (
	"fmt"
	"math/rand"
	"message-consolidator/logger"
	"message-consolidator/store"
	"sync"
	"time"
)

// GamificationResult encapsulates the rewards earned from a task. This data drives frontend visual feedback (like confetti or combo animations) to enhance user engagement.
type GamificationResult struct {
	XPAdded              int
	PointsAdded          int
	IsCritical           bool
	ComboActive          bool
	UnlockedAchievements []store.Achievement
}

var (
	gamificationMu sync.Mutex
	dirtyUsers     = make(map[string]*store.User)
)

// ProcessTaskCompletion handles XP, Points, Combo, and Streak logic when a task is finished.
func ProcessTaskCompletion(u *store.User) (GamificationResult, error) {
	now := time.Now()
	res := calculateRewards(u, now)

	u.XP += res.XPAdded
	u.Points += res.PointsAdded
	u.Level = (u.XP / 100) + 1

	newStreak, freezesUsed := updateStreak(u.Streak, u.StreakFreezes, u.LastCompletedAt, now)
	u.Streak = newStreak
	u.StreakFreezes -= freezesUsed
	u.LastCompletedAt = &now

	queueUserUpdate(u)

	//Why: Evaluates achievement conditions asynchronously to ensure the main task completion API responds quickly without being blocked by database queries.
	go func(user store.User) {
		unlocked, err := store.CheckAndUnlockAchievements(user)
		if err != nil {
			logger.Errorf("[Gamification] Background achievement check failed for %s: %v", user.Email, err)
			return
		}
		if len(unlocked) > 0 {
			logger.Infof("[Gamification] User %s unlocked %d achievements in background", user.Email, len(unlocked))
			//Why: [TODO] Trigger real-time push notifications (e.g., via WebSockets) here to immediately alert the user of newly unlocked achievements.
		}
	}(*u)

	return res, nil
}

// calculateRewards determines the XP and points awarded for completing a task.
// It includes base rewards, combo bonuses, and a chance for a critical hit.
func calculateRewards(u *store.User, now time.Time) GamificationResult {
	//Why: Grants base rewards for every successful task completion to provide consistent positive reinforcement.
	res := GamificationResult{XPAdded: 10, PointsAdded: 5}

	//Why: Incentivizes rapid task resolution by applying a combo bonus if the user completes another task within a 5-minute window.
	if u.LastCompletedAt != nil && now.Sub(*u.LastCompletedAt) < 5*time.Minute {
		res.ComboActive = true
		res.XPAdded += 5
		res.PointsAdded += 2
	}

	//Why: Adds a gamified "critical hit" mechanic with a 5% probability to randomly double rewards, enhancing user engagement.
	if rand.Float32() < 0.05 {
		res.IsCritical = true
		res.XPAdded *= 2
		res.PointsAdded *= 2
	}
	return res
}

// updateStreak calculates the new streak count and the number of freezes used.
func updateStreak(currentStreak, currentFreezes int, lastCompletedAt *time.Time, now time.Time) (newStreak int, freezesUsed int) {
	//Why: Initializes the user's streak to 1 upon their first-ever task completion.
	if lastCompletedAt == nil {
		return 1, 0
	}

	//Why: Normalizes both timestamps to the start of the day (midnight) to perform accurate date-based streak calculations.
	last := *lastCompletedAt
	lastDate := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, last.Location())
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daysDiff := int(nowDate.Sub(lastDate).Hours() / 24)

	switch {
	case daysDiff == 1:
		//Why: Streak continuation: The user completed a task on the following day, so the streak counter is incremented.
		return currentStreak + 1, 0
	case daysDiff > 1:
		//Why: Streak gap detection: The user missed one or more days, requiring a check for available streak freezes.
		missedDays := daysDiff - 1
		if currentFreezes >= missedDays {
			//Why: Consumes available 'streak freezes' to preserve and increment a streak even if the user missed one or more days.
			return currentStreak + 1, missedDays
		}
		//Why: Streak reset: Insufficient freezes are available to bridge the gap, so the streak restarts at 1.
		return 1, 0
	default: // daysDiff is 0 or negative (e.g., multiple tasks on the same day)
		//Why: Intra-day maintenance: Multiple tasks completed on the same day maintain the current streak without incrementing it.
		return currentStreak, 0
	}
}

// queueUserUpdate adds a user's updated gamification state to a "dirty" map,
// which will be batch-written to the database by FlushGamificationData.
func queueUserUpdate(u *store.User) {
	gamificationMu.Lock()
	defer gamificationMu.Unlock()
	//Why: Takes a full object snapshot of the user state before queuing to prevent race conditions during asynchronous database flushes.
	snapshot := *u
	dirtyUsers[u.Email] = &snapshot
	logger.Debugf("[Gamification] User %s queued for flush (XP: %d, Pts: %d)", u.Email, u.XP, u.Points)
}

// FlushGamificationData executes a batch update of all queued gamification state changes.
// It is designed to be piggybacked onto existing database activity (like message scanning) to minimize dedicated connection overhead.
func FlushGamificationData() error {
	gamificationMu.Lock()
	count := len(dirtyUsers)
	if count == 0 {
		gamificationMu.Unlock()
		return nil
	}
	//Why: Swaps the atomic "dirty" map with a fresh instance under lock to allow concurrent updates while the current batch is being processed.
	usersToUpdate := dirtyUsers
	dirtyUsers = make(map[string]*store.User)
	gamificationMu.Unlock()

	logger.Infof("[Gamification] Starting piggyback flush for %d users...", count)
	var errCount int
	for email, u := range usersToUpdate {
		err := store.UpdateUserGamification(email, u.Points, u.Streak, u.Level, u.XP, u.DailyGoal, u.LastCompletedAt, u.StreakFreezes)
		if err != nil {
			logger.Errorf("[Gamification] Failed to piggyback flush data for %s: %v", email, err)
			errCount++

			//Why: Implements a simple rollback by re-queuing failed updates, ensuring gamification progress is not lost due to transient database errors.
			gamificationMu.Lock()
			if _, exists := dirtyUsers[email]; !exists {
				dirtyUsers[email] = u
			}
			gamificationMu.Unlock()
		} else {
			logger.Debugf("[Gamification] Successfully flushed data for %s", email)
		}
	}

	if errCount > 0 {
		return fmt.Errorf("failed to flush gamification data for %d users", errCount)
	}
	logger.Infof("[Gamification] Piggyback flush completed successfully for %d users.", count)
	return nil
}
