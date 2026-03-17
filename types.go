package main

import (
	"time"
)

type RawChatMessage struct {
	ID             string
	User           string
	Sender         string
	InteractedUser string 
	Text           string
	Timestamp      time.Time
	Time           time.Time // Added for compatibility with whatsapp.go
	RawTS          string
}
