package services

import (
	"fmt"
	"sync"
)

// RoomLockService manages in-memory business locks per chat room (Room/JID).
// Why: [Concurrency] Ensures sequential processing of messages from the same source to prevent Upsert race conditions.
type RoomLockService struct {
	locks sync.Map
}

// NewRoomLockService creates a new instance of the lock service.
func NewRoomLockService() *RoomLockService {
	return &RoomLockService{}
}

// AcquireLock returns a persistent mutex for the given room key.
// Why: [Reliability] Uses sync.Map.LoadOrStore to keep the mutex pointer stable throughout the application lifecycle,
// avoiding Lock-reference Race conditions commonly seen with temporary mutex deletions.
func (s *RoomLockService) AcquireLock(key string) *sync.Mutex {
	actual, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// GetRoomKey generates a globally unique key for locking a specific chat room.
// Why: [Security] Isolates locks by user, platform source, and specific room/channel ID.
func (s *RoomLockService) GetRoomKey(userEmail, source, roomID string) string {
	return fmt.Sprintf("%s:%s:%s", userEmail, source, roomID)
}
