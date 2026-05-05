package config

import (
	"context"
	"errors"
	"message-consolidator/db"
	"os"
	"testing"
	"time"
)

func TestEnvOr(t *testing.T) {
	tests := []struct {
		name, key, envVal, fallback, want string
	}{
		{"env set", "TEST_ENVOR_A", "myval", "default", "myval"},
		{"env empty", "TEST_ENVOR_B", "", "default", "default"},
		{"env unset", "TEST_ENVOR_C_UNSET", "", "fallback", "fallback"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv(tt.key, tt.envVal)
			}
			if got := envOr(tt.key, tt.fallback); got != tt.want {
				t.Errorf("envOr(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		name, key, val string
		fallback, want int
	}{
		{"not set returns fallback", "TEST_ENVINT_A", "", 5, 5},
		{"valid int parsed", "TEST_ENVINT_B", "42", 0, 42},
		{"invalid string returns fallback", "TEST_ENVINT_C", "bad", 7, 7},
		{"TELEGRAM_APP_ID invalid logs warn", "TELEGRAM_APP_ID", "nope", 0, 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.val != "" {
				t.Setenv(tt.key, tt.val)
			}
			if got := envInt(tt.key, tt.fallback); got != tt.want {
				t.Errorf("envInt(%q) = %d, want %d", tt.key, got, tt.want)
			}
		})
	}
}

func TestEnvIntFirst(t *testing.T) {
	t.Run("last key wins", func(t *testing.T) {
		t.Setenv("EIF_KEY1", "10")
		t.Setenv("EIF_KEY2", "20")
		// envIntFirst iterates all keys; last non-fallback wins
		got := envIntFirst([]string{"EIF_KEY1", "EIF_KEY2"}, 0)
		if got != 20 {
			t.Errorf("got %d, want 20", got)
		}
	})
	t.Run("fallback when both missing", func(t *testing.T) {
		if got := envIntFirst([]string{"EIF_MISS1", "EIF_MISS2"}, 99); got != 99 {
			t.Errorf("got %d, want 99", got)
		}
	})
}

func TestEnvDuration(t *testing.T) {
	tests := []struct {
		name, key, val string
		fallback, want time.Duration
	}{
		{"not set returns fallback", "TEST_EDUR_A", "", 5 * time.Second, 5 * time.Second},
		{"valid duration parsed", "TEST_EDUR_B", "30s", 0, 30 * time.Second},
		{"invalid string returns fallback", "TEST_EDUR_C", "bad", 2 * time.Minute, 2 * time.Minute},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.val != "" {
				t.Setenv(tt.key, tt.val)
			}
			if got := envDuration(tt.key, tt.fallback); got != tt.want {
				t.Errorf("envDuration(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw string
		want      []string
	}{
		{"empty", "", nil},
		{"single", "foo", []string{"foo"}},
		{"multiple", "Foo,BAR, baz ", []string{"foo", "bar", "baz"}},
		{"trailing comma", "a,b,", []string{"a", "b"}},
		{"only commas", ",,,", nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitCSV(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCSV(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseBoolEnv(t *testing.T) {
	tests := []struct {
		name, key, val string
		fallback, want bool
	}{
		{"unset uses fallback true", "TEST_PBE_A", "", true, true},
		{"unset uses fallback false", "TEST_PBE_B", "", false, false},
		{"true string", "TEST_PBE_C", "true", false, true},
		{"1 string", "TEST_PBE_D", "1", false, true},
		{"yes string", "TEST_PBE_E", "YES", false, true},
		{"false string", "TEST_PBE_F", "false", true, false},
		{"0 string", "TEST_PBE_G", "0", true, false},
		{"no string", "TEST_PBE_H", "no", true, false},
		{"invalid uses fallback", "TEST_PBE_I", "maybe", true, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.val != "" {
				t.Setenv(tt.key, tt.val)
			}
			if got := parseBoolEnv(tt.key, tt.fallback); got != tt.want {
				t.Errorf("parseBoolEnv(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseIntCSV(t *testing.T) {
	t.Parallel()
	fallback := []int{24, 1}
	tests := []struct {
		name, raw string
		want      []int
	}{
		{"empty returns fallback", "", fallback},
		{"single valid", "5", []int{5}},
		{"multiple valid", "1,2,3", []int{1, 2, 3}},
		{"invalid returns fallback", "1,bad,3", fallback},
		{"spaces trimmed", " 7 , 8 ", []int{7, 8}},
		{"all empty parts returns fallback", ",,,", fallback},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseIntCSV(tt.raw, fallback)
			if len(got) != len(tt.want) {
				t.Fatalf("parseIntCSV(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"", false},
		{"maybe", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := parseBool(tt.in); got != tt.want {
				t.Errorf("parseBool(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetIntIfValid(t *testing.T) {
	t.Parallel()
	t.Run("valid sets value", func(t *testing.T) {
		t.Parallel()
		n := 5
		setIntIfValid(&n, "42")
		if n != 42 {
			t.Errorf("got %d, want 42", n)
		}
	})
	t.Run("invalid leaves unchanged", func(t *testing.T) {
		t.Parallel()
		n := 5
		setIntIfValid(&n, "bad")
		if n != 5 {
			t.Errorf("got %d, want 5", n)
		}
	})
}

func TestSetDurationIfValid(t *testing.T) {
	t.Parallel()
	t.Run("valid sets value", func(t *testing.T) {
		t.Parallel()
		d := time.Second
		setDurationIfValid(&d, "30s")
		if d != 30*time.Second {
			t.Errorf("got %v, want 30s", d)
		}
	})
	t.Run("invalid leaves unchanged", func(t *testing.T) {
		t.Parallel()
		d := time.Second
		setDurationIfValid(&d, "bad")
		if d != time.Second {
			t.Errorf("got %v, want 1s", d)
		}
	})
}


func TestApplyOverlay(t *testing.T) {
	t.Parallel()
	cfg := &Config{LogLevel: "INFO", AutoArchiveDays: 7}
	applyOverlay(cfg, map[string]string{
		"LOG_LEVEL":    "DEBUG",
		"ARCHIVE_DAYS": "30",
		"UNKNOWN_KEY":  "ignored",
	})
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG", cfg.LogLevel)
	}
	if cfg.AutoArchiveDays != 30 {
		t.Errorf("AutoArchiveDays = %d, want 30", cfg.AutoArchiveDays)
	}
}

func TestDurationOrSecondsValidator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"7s", false},
		{"30m", false},
		{"10", false},
		{"0", false},
		{"bad", true},
		{"", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			err := durationOrSecondsValidator(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("durationOrSecondsValidator(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestLoadDotenv_WithEnvLocal(t *testing.T) {
	dir := t.TempDir()
	envLocal := dir + "/.env.local"
	if err := os.WriteFile(envLocal, []byte("TEST_DOTENV_VAR=fromfile\n"), 0o600); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}
	t.Chdir(dir)
	loadDotenv()
	if got := os.Getenv("TEST_DOTENV_VAR"); got != "fromfile" {
		t.Errorf("TEST_DOTENV_VAR = %q, want fromfile", got)
	}
}

func TestLoadDotenv_EnvLocalUnreadable(t *testing.T) {
	dir := t.TempDir()
	envLocal := dir + "/.env.local"
	if err := os.WriteFile(envLocal, []byte("KEY=val\n"), 0o000); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}
	t.Chdir(dir)
	// Should not panic — error branch logs a warning and returns.
	loadDotenv()
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	// Why: project root .env contains ARCHIVE_DAYS=3 which would override AUTO_ARCHIVE_DAYS
	// via envIntFirst's last-wins iteration. Isolate to a clean temp dir without dotenv files.
	t.Chdir(t.TempDir())
	t.Setenv("SLACK_TOKEN", "test-token")
	t.Setenv("AUTH_DISABLED", "true")
	t.Setenv("AUTO_ARCHIVE_DAYS", "14")
	t.Setenv("ARCHIVE_DAYS", "")
	t.Setenv("LOG_LEVEL", "DEBUG")
	cfg := LoadConfig()
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if cfg.SlackToken != "test-token" {
		t.Errorf("SlackToken = %q, want test-token", cfg.SlackToken)
	}
	if !cfg.AuthDisabled {
		t.Errorf("AuthDisabled = false, want true")
	}
	if cfg.AutoArchiveDays != 14 {
		t.Errorf("AutoArchiveDays = %d, want 14", cfg.AutoArchiveDays)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG", cfg.LogLevel)
	}
}

func TestOverlayFromDB_NilGuards(t *testing.T) {
	t.Parallel()
	loader := func(_ context.Context) ([]db.AppSetting, error) { return nil, nil }
	if err := OverlayFromDB(context.Background(), nil, loader); err != nil {
		t.Errorf("nil cfg should be no-op, got %v", err)
	}
	if err := OverlayFromDB(context.Background(), &Config{}, nil); err != nil {
		t.Errorf("nil loader should be no-op, got %v", err)
	}
}

func TestOverlayFromDB_EmptyRows(t *testing.T) {
	t.Parallel()
	cfg := &Config{LogLevel: "INFO"}
	err := OverlayFromDB(context.Background(), cfg, func(_ context.Context) ([]db.AppSetting, error) {
		return nil, nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("config should be unchanged, got %q", cfg.LogLevel)
	}
}

func TestOverlayFromDB_LoadError(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	loadErr := errors.New("db unavailable")
	err := OverlayFromDB(context.Background(), cfg, func(_ context.Context) ([]db.AppSetting, error) {
		return nil, loadErr
	})
	if !errors.Is(err, loadErr) {
		t.Errorf("expected load error, got %v", err)
	}
}

func TestOverlayFromDB_AppliesRows(t *testing.T) {
	t.Parallel()
	cfg := &Config{LogLevel: "INFO", AutoArchiveDays: 7}
	err := OverlayFromDB(context.Background(), cfg, func(_ context.Context) ([]db.AppSetting, error) {
		return []db.AppSetting{
			{Key: "LOG_LEVEL", Value: "DEBUG"},
			{Key: "ARCHIVE_DAYS", Value: "30"},
			{Key: "UNKNOWN_KEY", Value: "ignored"},
			{Key: "LOG_LEVEL", Value: "  "}, // whitespace-only: should be skipped
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want DEBUG", cfg.LogLevel)
	}
	if cfg.AutoArchiveDays != 30 {
		t.Errorf("AutoArchiveDays = %d, want 30", cfg.AutoArchiveDays)
	}
}
