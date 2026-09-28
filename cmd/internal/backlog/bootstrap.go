package backlog

import (
	"context"
	"fmt"

	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
)

// Env is the shared bootstrap output every reassess-*-backlog tool builds its
// channel-specific setup (Slack/WhatsApp/Gmail client, task listing) on top of.
type Env struct {
	Cfg           *config.Config
	Store         *Store
	CompletionSvc *services.CompletionService
}

// Bootstrap runs the init sequence common to every reassess-*-backlog tool: load
// config, init token encryption, open the DB, resolve the configured AI provider, and
// wire the write-intercepting Store into a CompletionService. apply gates whether Store
// persists confirm-first candidates; source labels its audit Evidence string (e.g.
// "slack", "whatsapp", "gmail").
func Bootstrap(ctx context.Context, source string, apply bool) (*Env, error) {
	cfg := config.LoadConfig()
	// Why: without the key, any encrypted token column is read back as ciphertext.
	store.InitTokenEncryption()
	if err := store.InitDB(ctx, cfg); err != nil {
		return nil, fmt.Errorf("DB init failed: %w", err)
	}

	pc := ProviderConfig(cfg)
	if !pc.Enabled() {
		return nil, fmt.Errorf("no AI provider configured (GEMINI_API_KEY / DEEPSEEK_API_KEY)")
	}
	aiClient, err := ai.NewAIClient(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("AI client init failed: %w", err)
	}

	bs := NewStore(&services.DefaultTaskStore{}, store.GetDB(), apply, source)
	completionSvc := services.NewCompletionService(aiClient, bs, &services.TasksService{}, store.GetDB())

	return &Env{Cfg: cfg, Store: bs, CompletionSvc: completionSvc}, nil
}

// Finish prints Store's audit table and the apply/dry-run footer every
// reassess-*-backlog tool prints at the end of main.
func (s *Store) Finish(apply bool) {
	s.PrintResults()
	if apply {
		fmt.Printf("\nwrote %d confirm-first candidate(s)\n", s.Written())
	} else {
		fmt.Println("\ndry run: no writes (pass -apply to record confirm-first candidates)")
	}
}
