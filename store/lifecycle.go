package store

// Lifecycle states mirror the generated `messages.lifecycle` column (store/migrations.go,
// migrateLifecycleExcluded). Single source of truth for both the Go-side mirror
// (messageLifecycle) and any code branching on lifecycle string literals.
const (
	LifecycleActive   = "active"
	LifecycleDone     = "done"
	LifecycleSwept    = "swept"
	LifecycleCanceled = "canceled"
	LifecycleMerged   = "merged"
	LifecycleExcluded = "excluded"
)
