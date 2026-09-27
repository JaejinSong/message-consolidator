package services

import (
	"sync/atomic"
)

// completionStats aggregates per-cycle counters for the completion decision
// funnel (thread-path vs cross-channel, FTS gate, LLM outcomes, confirm-first
// candidate lifecycle) so drop-off can be diagnosed from logs without re-instrumenting.
type completionStats struct {
	entryThreadPath        atomic.Int64
	crossSignalMiss        atomic.Int64
	ftsEmpty               atomic.Int64
	llmError               atomic.Int64
	llmResolve             atomic.Int64
	llmUpdate              atomic.Int64
	crossRoomUpdateSkipped atomic.Int64
	llmNone                atomic.Int64
	topicalMiss            atomic.Int64
	candidateRecorded      atomic.Int64
	dismissSuppressed      atomic.Int64
	fallbackExtraction     atomic.Int64
}

var compStats completionStats
