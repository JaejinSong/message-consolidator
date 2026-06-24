package core

import (
	"errors"
	"strings"
	"testing"
)

func TestParsedPrompt_Render(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		data    any
		want    string
		wantErr bool
	}{
		{
			name: "Renders Struct Fields",
			body: "user={{.CurrentUser}}, payload={{.MessagePayload}}",
			data: ExtractionContext{CurrentUser: "alice", MessagePayload: "p1"},
			want: "user=alice, payload=p1",
		},
		{
			name: "Renders Map Data",
			body: "k={{.k}}",
			data: map[string]string{"k": "v"},
			want: "k=v",
		},
		{
			name:    "Invalid Template Syntax Returns Parse Error",
			body:    "Hello {{.Name",
			data:    map[string]string{},
			wantErr: true,
		},
		{
			name:    "Execute Error On Missing Struct Field",
			body:    "{{.NotARealField}}",
			data:    ExtractionContext{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &ParsedPrompt{Body: tt.body}
			got, err := p.Render(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (output=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.want) && got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
geminiModel: gemini-3-flash
deepseekModel: deepseek-chat
---
Hello, World!`,
			wantErr: nil,
			wantMeta: PromptMeta{
				Name:          "test-prompt",
				Version:       "v1.0",
				GeminiModel:   "gemini-3-flash",
				DeepSeekModel: "deepseek-chat",
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
			name:    "Empty content",
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
