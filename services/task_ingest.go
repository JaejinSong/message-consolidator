package services

import (
	"context"
	"message-consolidator/store"
)

// GuardAndBuild runs ApplyExtractionGuard and, when the item survives, BuildTask on
// the guarded params. Why: chat (scanner) and Gmail ingestion both need the identical
// guard-then-build sequence before routing through HandleTaskState; sharing it here
// keeps the two paths from drifting while leaving logging and routing (querier choice,
// ordering, Gmail's two-phase collect-then-route) to each caller.
func GuardAndBuild(ctx context.Context, params TaskBuildParams) (store.TodoItem, store.ConsolidatedMessage, GuardResult, bool) {
	guardedParams, guard := ApplyExtractionGuard(ctx, params)
	if !guard.Kept {
		return store.TodoItem{}, store.ConsolidatedMessage{}, guard, false
	}
	msg := BuildTask(ctx, guardedParams)
	return guardedParams.Item, msg, guard, true
}
