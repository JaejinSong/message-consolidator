package ai

import (
	"errors"
	"testing"
)

func TestParsePrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		wantErr  error
		wantMeta PromptMeta
		wantBody string
	}{
		{
			name: "Standard Frontmatter",
			content: `---
Name: test-prompt
Version: v1.0
Model: gemini-3-flash
---
Hello, World!`,
			wantErr: nil,
			wantMeta: PromptMeta{
				Name:    "test-prompt",
				Version: "v1.0",
				Model:   "gemini-3-flash",
			},
			wantBody: "Hello, World!",
		},
		{
			name: "Strict Prefix Violation (Leading Garbage)",
			content: `// Garbage
---
Name: test
---
Body`,
			wantErr: ErrInvalidFrontmatter,
		},
		{
			name: "Missing Closing Separator",
			content: `---
Name: test
Body without second separator`,
			wantErr: ErrInvalidFrontmatter,
		},
		{
			name: "Case Insensitive Keys and Extra Spaces",
			content: `---
  NAME  :  spaced-name  
  version : 2.0  
---
Body`,
			wantErr: nil,
			wantMeta: PromptMeta{
				Name:    "spaced-name",
				Version: "2.0",
			},
			wantBody: "Body",
		},
		{
			name: "Empty content",
			content: "",
			wantErr: ErrInvalidFrontmatter,
		},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePrompt(tt.content)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ParsePrompt() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePrompt() unexpected error = %v", err)
			}
			if got.Meta != tt.wantMeta {
				t.Errorf("ParsePrompt() Meta = %+v, want %+v", got.Meta, tt.wantMeta)
			}
			if got.Body != tt.wantBody {
				t.Errorf("ParsePrompt() Body = %q, want %q", got.Body, tt.wantBody)
			}
		})
	}
}
