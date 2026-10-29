package ai

import "context"

// FilterInput is what a noise filter needs to judge one payload and to attribute its verdict.
type FilterInput struct {
	Email      string
	Source     string
	Room       string
	SourceTS   string
	MessageRef string
	Text       string
}

// Why: the legacy LLM filter ignores Room/SourceTS/MessageRef; this adapter keeps its logic untouched.
func (f *LiteFilter) IsNoise(ctx context.Context, in FilterInput) (bool, error) {
	return f.isNoiseText(ctx, in.Email, in.Source, in.Text)
}
