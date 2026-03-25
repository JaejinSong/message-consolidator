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

	// 비동기로 업적 체크 진행 (API 응답 지연 방지)
	go func(user store.User) {
		unlocked, err := store.CheckAndUnlockAchievements(user)
		if err != nil {
			logger.Errorf("[Gamification] Background achievement check failed for %s: %v", user.Email, err)
			return
		}
		if len(unlocked) > 0 {
			logger.Infof("[Gamification] User %s unlocked %d achievements in background", user.Email, len(unlocked))
			// TODO: 실시간 알림(WebSocket 등)이 필요하다면 여기서 트리거
		}
	}(*u)

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
	logger.Debugf("[Gamification] User %s queued for flush (XP: %d, Pts: %d)", u.Email, u.XP, u.Points)
}

// FlushGamificationData 는 DB가 스캔 등의 이유로 이미 깨어있는 시점(Piggyback)에 호출되어
// 큐에 쌓인 변경사항을 한 번에 배치 처리합니다.
func FlushGamificationData() error {
	gamificationMu.Lock()
	count := len(dirtyUsers)
	if count == 0 {
		gamificationMu.Unlock()
		return nil
	}
	// 안전한 복사 후 큐 비우기
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

			// 롤백: DB 저장에 실패한 유저는 다음 Flush 를 위해 큐에 복구
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
