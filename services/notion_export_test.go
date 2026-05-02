package services

import (
	"strings"
	"testing"
)

func TestMarkdownToNotionBlocks_JsonTable(t *testing.T) {
	md := "## [Activity]\n```json\n[\n  {\"customer\": \"Bank BNI\", \"count\": 16},\n  {\"customer\": \"Netciti\", \"count\": 7}\n]\n```"
	blocks := markdownToNotionBlocks(md)

	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks (heading + table), got %d", len(blocks))
	}

	tableBlock := blocks[1]
	if tableBlock["type"] != "table" {
		t.Errorf("expected table block, got %s", tableBlock["type"])
	}

	table := tableBlock["table"].(map[string]any)
	rows := table["children"].([]map[string]any)
	if len(rows) != 3 { // 1 header + 2 data rows
		t.Errorf("expected 3 rows (header + 2 data), got %d", len(rows))
	}
}

func TestMarkdownToNotionBlocks_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	blocks := markdownToNotionBlocks(md)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(blocks))
	}
	if blocks[0]["type"] != "code" {
		t.Errorf("expected code block, got %s", blocks[0]["type"])
	}
}

func TestMarkdownToNotionBlocks_Headings(t *testing.T) {
	md := "# H1\n## H2\n### H3\n- bullet\nplain text"
	blocks := markdownToNotionBlocks(md)

	types := []string{"heading_1", "heading_2", "heading_3", "bulleted_list_item", "paragraph"}
	if len(blocks) != len(types) {
		t.Fatalf("expected %d blocks, got %d", len(types), len(blocks))
	}
	for i, b := range blocks {
		if b["type"] != types[i] {
			t.Errorf("block[%d]: expected %s, got %s", i, types[i], b["type"])
		}
	}
}

func TestRichText_LongText(t *testing.T) {
	long := strings.Repeat("a", 5000)
	parts := richText(long)
	if len(parts) != 3 { // 2000 + 2000 + 1000
		t.Errorf("expected 3 parts for 5000-char text, got %d", len(parts))
	}
}

func TestJsonArrayToTable_InvalidJSON(t *testing.T) {
	if jsonArrayToTable("not json") != nil {
		t.Error("expected nil for invalid JSON")
	}
	if jsonArrayToTable("{}") != nil {
		t.Error("expected nil for non-array JSON")
	}
}

func TestNotionLang(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"json", "json"},
		{"JSON", "json"},
		{"  go ", "go"},
		{"js", "javascript"},
		{"javascript", "javascript"},
		{"ts", "typescript"},
		{"typescript", "typescript"},
		{"", "plain text"},
		{"perl", "plain text"},
	}
	for _, tt := range tests {
		if got := notionLang(tt.in); got != tt.want {
			t.Errorf("notionLang(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-3, -1, -3},
		{5, 5, 5},
	}
	for _, tt := range tests {
		if got := min(tt.a, tt.b); got != tt.want {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestNewNotionExporter_Enabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, token, parent string
		want                bool
	}{
		{"both set", "tok", "page", true},
		{"missing token", "", "page", false},
		{"missing parent", "tok", "", false},
		{"both missing", "", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := NewNotionExporter(tt.token, tt.parent)
			if n == nil {
				t.Fatal("constructor returned nil")
			}
			if got := n.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
