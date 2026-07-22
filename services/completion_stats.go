package services

import (
	"sync/atomic"

	"message-consolidator/logger"
)

// completionStats aggregates per-cycle counters for the completion decision
// funnel (thread-path vs cross-channel, FTS gate, LLM outcomes, confirm-first
// candidate lifecycle) so drop-off can be diagnosed from logs without re-instrumenting.
type completionStats struct {
	entryThreadPath    atomic.Int64
	crossSignalMiss    atomic.Int64
	ftsEmpty           atomic.Int64
	llmError           atomic.Int64
	llmResolve         atomic.Int64
	llmUpdate          atomic.Int64
	llmNone            atomic.Int64
	topicalMiss        atomic.Int64
	candidateRecorded  atomic.Int64
	dismissSuppressed  atomic.Int64
	fallbackExtraction atomic.Int64
}

var compStats completionStats

// LogCompletionStats emits one summary line for the completion pipeline funnel,
// then resets all counters so the next window starts clean.
func LogCompletionStats() {
	logger.Infof("[COMPLETION] stats: entryThreadPath=%d crossSignalMiss=%d ftsEmpty=%d llmError=%d llmResolve=%d llmUpdate=%d llmNone=%d topicalMiss=%d candidateRecorded=%d dismissSuppressed=%d fallbackExtraction=%d",
		compStats.entryThreadPath.Swap(0),
		compStats.crossSignalMiss.Swap(0),
		compStats.ftsEmpty.Swap(0),
		compStats.llmError.Swap(0),
		compStats.llmResolve.Swap(0),
		compStats.llmUpdate.Swap(0),
		compStats.llmNone.Swap(0),
		compStats.topicalMiss.Swap(0),
		compStats.candidateRecorded.Swap(0),
		compStats.dismissSuppressed.Swap(0),
		compStats.fallbackExtraction.Swap(0),
	)
}
