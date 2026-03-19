package main

import "time"

// RawMessage represents a generic text message extracted from any source (Slack, WhatsApp, etc.)
type RawMessage struct {
	ID        string
	Sender    string
	Text      string
	Timestamp time.Time
}
