package services

import (
	"fmt"
	"math/rand"
	"message-consolidator/logger"
	"message-consolidator/store"
	"sync"
	"time"
)

// GamificationResult 에는 프론트엔드 연출(꽃가루, 연속 콤보 이펙트)을 위한 정보가 담깁니다.
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

	unlocked, err := store.CheckAndUnlockAchievements(*u)
	if err == nil && len(unlocked) > 0 {
		res.UnlockedAchievements = unlocked
	}

	return res, nil
}

func calculateRewards(u *store.User, now time.Time) GamificationResult {
	res := GamificationResult{XPAdded: 10, PointsAdded: 5}

	if u.LastCompletedAt != nil && now.Sub(*u.LastCompletedAt) < 5*time.Minute {
		res.ComboActive = true
		res.XPAdded += 5
		res.PointsAdded += 2
	}

	if rand.Float32() < 0.05 {
		res.IsCritical = true
		res.XPAdded *= 2
		res.PointsAdded *= 2
	}
	return res
}

func updateStreak(currentStreak, currentFreezes int, lastCompletedAt *time.Time, now time.Time) (int, int) {
	if lastCompletedAt == nil {
		return 1, 0
	}
	last := *lastCompletedAt
	lastDate := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, last.Location())
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daysDiff := int(nowDate.Sub(lastDate).Hours() / 24)

	if daysDiff == 1 {
		return currentStreak + 1, 0
	} else if daysDiff > 1 {
		missedDays := daysDiff - 1
		if currentFreezes >= missedDays {
			// 보호권이 충분하면 공백을 메우고 오늘 분(+1) 추가
			return currentStreak + 1, missedDays
		}
		return 1, 0
	}
	return currentStreak, 0
}

func queueUserUpdate(u *store.User) {
	gamificationMu.Lock()
	defer gamificationMu.Unlock()
	snapshot := *u
	dirtyUsers[u.Email] = &snapshot
}

// FlushGamificationData 는 DB가 스캔 등의 이유로 이미 깨어있는 시점(Piggyback)에 호출되어
// 큐에 쌓인 변경사항을 한 번에 배치 처리합니다.
func FlushGamificationData() error {
	gamificationMu.Lock()
	if len(dirtyUsers) == 0 {
		gamificationMu.Unlock()
		return nil
	}
	// 안전한 복사 후 큐 비우기
	usersToUpdate := dirtyUsers
	dirtyUsers = make(map[string]*store.User)
	gamificationMu.Unlock()

	var errCount int
	for email, u := range usersToUpdate {
		err := store.UpdateUserGamification(email, u.Points, u.Streak, u.Level, u.XP, u.DailyGoal, u.LastCompletedAt, u.StreakFreezes)
		if err != nil {
			logger.Errorf("[Gamification] Failed to piggyback flush data for %s: %v", email, err)
			errCount++

			// 롤백: DB 저장에 실패한 유저는 다음 Flush 를 위해 큐에 복구
			gamificationMu.Lock()
			if _, exists := dirtyUsers[email]; !exists {
				dirtyUsers[email] = u
			}
			gamificationMu.Unlock()
		}
	}

	if errCount > 0 {
		return fmt.Errorf("failed to flush gamification data for %d users", errCount)
	}
	return nil
}
