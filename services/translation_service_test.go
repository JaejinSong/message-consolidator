package services

import (
	"context"
	"testing"
)

func TestGetLanguageName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"ko", "Korean"},
		{"KO", "Korean"},
		{"en", "English"},
		{"id", "Indonesian"},
		{"th", "Thai"},
		{"ja", "ja"}, // Why: unknown codes pass through verbatim so prompt still mentions a target.
		{"", ""},
	}
	for _, tt := range tests {
		if got := GetLanguageName(tt.in); got != tt.want {
			t.Errorf("GetLanguageName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewTranslationService_Defaults(t *testing.T) {
	t.Parallel()
	s := NewTranslationService(nil)
	if s == nil {
		t.Fatal("constructor returned nil")
	}
	if cap(s.semaphore) != 5 {
		t.Errorf("semaphore cap = %d, want 5 (concurrent AI call ceiling)", cap(s.semaphore))
	}
}

func TestTranslate_NilGeminiReturnsError(t *testing.T) {
	t.Parallel()
	s := NewTranslationService(nil)
	if _, err := s.Translate(context.Background(), "u@x.com", "k", "text", "ko", false, 0); err == nil {
		t.Error("expected error when gemini client is nil")
	}
}

func TestTranslateBatch_NoTasksReturnsNil(t *testing.T) {
	t.Parallel()
	s := NewTranslationService(nil)
	got, err := s.TranslateBatch(context.Background(), "u@x.com", nil, "ko")
	if err != nil {
		t.Errorf("nil gemini + empty tasks should not error, got %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
