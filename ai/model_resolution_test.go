package ai

import (
	"message-consolidator/ai/core"
	"testing"
)

// TestResolveModelThinking pins the per-provider model+thinking resolution for every
// model-resolution-source prompt. The frontmatter (geminiModel/geminiThinking/
// deepseekModel/deepseekThinking) is authoritative; the values below mirror the prior
// per-stage modelSpec defaults, so this guards that the frontmatter migration preserved
// behavior. lite_filter omits the gemini fields and must fall back to the spec (no-op).
func TestResolveModelThinking(t *testing.T) {
	type want struct {
		model    string
		thinking ThinkingMode
	}
	cases := []struct {
		name   string
		prompt core.PromptName
		spec   modelSpec // fallback when frontmatter omits a provider (lite_filter gemini)
		gemini want
		deep   want
	}{
		{"chat_system", core.PromptChatSystem, modelSpec{}, want{"gemini-3-flash-preview", ThinkOn}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"gmail_system", core.PromptGmailSystem, modelSpec{}, want{"gemini-3-flash-preview", ThinkOn}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"notion_system", core.PromptNotionSystem, modelSpec{}, want{"gemini-3-flash-preview", ThinkOn}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"report_summary", core.PromptReportSummary, modelSpec{}, want{"gemini-3-flash-preview", ThinkOff}, want{"deepseek-v4-pro", ThinkOn}},
		{"completion_check", core.PromptCompletionCheck, modelSpec{}, want{"gemini-3-flash-preview", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOn}},
		{"task_merge_summary", core.PromptTaskMergeSummary, modelSpec{}, want{"gemini-3-flash-preview", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"translation_system", core.PromptTranslationSystem, modelSpec{}, want{"gemini-3.1-flash-lite", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"report_translator", core.PromptReportTranslator, modelSpec{}, want{"gemini-3.1-flash-lite", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"task_translator", core.PromptTaskTranslator, modelSpec{}, want{"gemini-3.1-flash-lite", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"batch_translator", core.PromptBatchTranslator, modelSpec{}, want{"gemini-3.1-flash-lite", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
		{"identity_group_merge", core.PromptIdentityGroupMerge, modelSpec{}, want{"gemini-3-flash-preview", ThinkOn}, want{"deepseek-v4-flash:0731", ThinkOn}},
		// gemini fields omitted -> spec fallback (filter is a Gemini no-op: empty model, ThinkDefault).
		{"lite_filter", core.PromptLiteFilter, modelSpec{model: "", thinking: ThinkDefault}, want{"", ThinkDefault}, want{"deepseek-v4-flash:0731", ThinkOff}},
	}

	gem := &AIClient{provider: providerGemini}
	ds := &AIClient{provider: providerDeepSeek}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := core.LoadPrompt(tc.prompt)
			if got := gem.resolveModel(p, tc.spec); got != tc.gemini.model {
				t.Errorf("gemini model = %q, want %q", got, tc.gemini.model)
			}
			if got := gem.resolveThinking(p, tc.spec); got != tc.gemini.thinking {
				t.Errorf("gemini thinking = %v, want %v", got, tc.gemini.thinking)
			}
			if got := ds.resolveModel(p, tc.spec); got != tc.deep.model {
				t.Errorf("deepseek model = %q, want %q", got, tc.deep.model)
			}
			if got := ds.resolveThinking(p, tc.spec); got != tc.deep.thinking {
				t.Errorf("deepseek thinking = %v, want %v", got, tc.deep.thinking)
			}
		})
	}
}
