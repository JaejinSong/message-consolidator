// Package safego provides goroutine panic recovery helpers for fire-and-forget
// background work. CLAUDE.md requires every goroutine to either be ctx-cancellable
// or to have a done channel; safego complements that with panic isolation so a
// runaway goroutine never crashes the process or corrupts shared state silently.
package safego

import (
	"runtime/debug"

	"message-consolidator/logger"
)

// Recover is meant to be used as `defer safego.Recover("scanner-room-extract")`.
// It catches any panic, logs the stack trace with the supplied tag, and prevents
// the panic from terminating the process. Returns silently when no panic occurred.
func Recover(name string) {
	r := recover()
	if r == nil {
		return
	}
	logger.Errorf("[PANIC] goroutine %q recovered: %v\n%s", name, r, debug.Stack())
}
