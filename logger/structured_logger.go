package logger

import (
	"encoding/json"
	"time"
)

// DecisionLog represents a structured log entry for task routing decisions.
type DecisionLog struct {
	Timestamp time.Time `json:"timestamp"`
	UserEmail string    `json:"user_email"`
	Source    string    `json:"source"`
	Room      string    `json:"room"`
	State     string    `json:"state"`
	TaskID    *int64    `json:"task_id,omitempty"`
	Task      string    `json:"task"`
	Reasoning string    `json:"reasoning,omitempty"`
}

// LogDecision outputs a structured JSON log for WhaTap/CloudWatch monitoring.
// Why: [Diagnosis] Enables precise tracking of Silent Failures during the extraction-routing lifecycle.
func LogDecision(log DecisionLog) {
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}
	data, err := json.Marshal(log)
	if err != nil {
		Errorf("[DIAG] Failed to marshal decision log: %v", err)
		return
	}
	// Why: Use Infof directly as our logger setup already handles standard outputs and rotate.
	Infof("[DECISION] %s", string(data))
}
