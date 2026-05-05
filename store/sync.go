package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/internal/primes"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"time"

	turso "turso.tech/database/tursogo"

	"github.com/whatap/go-api/trace"
)

var tursoDB *turso.TursoSyncDb

// InitSyncDB opens an embedded replica, pulls latest state from remote, and returns the SQL connection.
// BootstrapIfEmpty is true by default (NewTursoSyncDb bootstraps on first open); explicit Pull ensures
// latest remote state even on post-restart when the local file already exists.
func InitSyncDB(ctx context.Context, cfg *config.Config) (*sql.DB, error) {
	db, err := turso.NewTursoSyncDb(ctx, turso.TursoSyncDbConfig{
		Path:                 cfg.TursoLocalDBPath,
		RemoteUrl:            cfg.TursoURL,
		AuthToken:            cfg.TursoToken,
		ExperimentalFeatures: "generated_columns",
	})
	if err != nil {
		return nil, fmt.Errorf("init sync db: %w", err)
	}

	// Why: Bootstrap only pulls on empty DB; explicit Pull guarantees latest state on every restart.
	if _, err := db.Pull(ctx); err != nil {
		return nil, fmt.Errorf("initial pull: %w", err)
	}

	sqlDB, err := db.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("sync connect: %w", err)
	}

	tursoDB = db
	return sqlDB, nil
}

// StartSyncLoop starts a background goroutine that periodically pushes local WAL changes to remote.
func StartSyncLoop(ctx context.Context, intervalStr string) {
	go func() {
		defer safego.Recover("db-sync-loop")

		var interval time.Duration
		if intervalStr != "" {
			if d, err := time.ParseDuration(intervalStr); err == nil {
				interval = d
			}
		}
		if interval == 0 {
			interval = primes.Pick(primes.Seconds)
		}

		timer := time.NewTimer(interval)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				pushSync(ctx)
				timer.Reset(primes.Pick(primes.Seconds))
			}
		}
	}()
}

func pushSync(ctx context.Context) {
	if tursoDB == nil {
		return
	}
	traceCtx, _ := trace.Start(ctx, "/Background-Infra-DBSyncPush")
	err := tursoDB.Push(traceCtx)
	_ = trace.End(traceCtx, err)
	if err != nil {
		logger.Warnf("[DB] sync: push failed: %v", err)
	}
}

// FlushAndClose pushes pending local changes and checkpoints WAL. Call during graceful shutdown.
func FlushAndClose(ctx context.Context) {
	if tursoDB == nil {
		return
	}
	if err := tursoDB.Push(ctx); err != nil {
		logger.Warnf("[DB] sync: final push failed: %v", err)
	}
	if err := tursoDB.Checkpoint(ctx); err != nil {
		logger.Warnf("[DB] sync: final checkpoint failed: %v", err)
	}
}
