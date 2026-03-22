package services

import (
	"context"
	"message-consolidator/store"
)

// HandleTaskCompletion orchestrates the process of marking a task as done,
// updating gamification stats, and potentially recording statistics for analytics.
func HandleTaskCompletion(email string, taskID int, done bool) (GamificationResult, error) {
	// 중복 보상 방지: 현재 상태 확인
	msg, err := store.GetMessageByID(context.Background(), taskID)
	if err == nil && msg.Done && done {
		// 이미 완료된 업무를 다시 완료로 표시하는 경우 보상 생략
		return GamificationResult{}, nil
	}

	if err := store.MarkMessageDone(email, taskID, done); err != nil {
		return GamificationResult{}, err
	}

	if !done {
		return GamificationResult{}, nil
	}

	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		return GamificationResult{}, err
	}

	return ProcessTaskCompletion(user)
}
