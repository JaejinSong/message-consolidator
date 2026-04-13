package services

import (
	"testing"
)

func TestRoomLockService_AcquireLock(t *testing.T) {
	svc := NewRoomLockService()

	key1 := "user@email.com:whatsapp:room1"
	key2 := "user@email.com:whatsapp:room2"

	lock1a := svc.AcquireLock(key1)
	lock1b := svc.AcquireLock(key1)

	if lock1a != lock1b {
		t.Errorf("AcquireLock returned different mutexes for the same key")
	}

	lock2 := svc.AcquireLock(key2)
	if lock1a == lock2 {
		t.Errorf("AcquireLock returned same mutex for different keys")
	}
}

func TestRoomLockService_GetRoomKey(t *testing.T) {
	svc := NewRoomLockService()
	email := "test@example.com"
	source := "slack"
	room := "C12345"

	expected := "test@example.com:slack:C12345"
	actual := svc.GetRoomKey(email, source, room)

	if actual != expected {
		t.Errorf("GetRoomKey failed: expected %s, got %s", expected, actual)
	}
}
