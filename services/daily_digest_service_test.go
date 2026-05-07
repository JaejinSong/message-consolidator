package services

import (
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

func TestComputeDailyWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	cases := []struct {
		name      string
		now       time.Time
		wantStart string
		wantEnd   string
	}{
		{"tuesday", time.Date(2026, 4, 28, 18, 0, 0, 0, loc), "2026-04-28", "2026-04-28"},
		{"wednesday", time.Date(2026, 4, 29, 18, 0, 0, 0, loc), "2026-04-29", "2026-04-29"},
		{"thursday", time.Date(2026, 4, 30, 18, 0, 0, 0, loc), "2026-04-30", "2026-04-30"},
		{"friday", time.Date(2026, 5, 1, 18, 0, 0, 0, loc), "2026-05-01", "2026-05-01"},
		{"monday_includes_weekend", time.Date(2026, 5, 4, 18, 0, 0, 0, loc), "2026-05-02", "2026-05-04"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := computeDailyWindow(tc.now)
			if gotStart != tc.wantStart {
				t.Errorf("start: want %q got %q", tc.wantStart, gotStart)
			}
			if gotEnd != tc.wantEnd {
				t.Errorf("end: want %q got %q", tc.wantEnd, gotEnd)
			}
		})
	}
}

func TestFormatDailyDMText(t *testing.T) {
	t.Run("same_day", func(t *testing.T) {
		got := formatDailyDMText("2026-04-28", "2026-04-28", "summary body")
		for _, want := range []string{"Daily Report", "2026-04-28", "summary body"} {
			if !containsString(got, want) {
				t.Errorf("result %q missing %q", got, want)
			}
		}
		if containsString(got, "~") {
			t.Errorf("same-day form should not contain '~': %q", got)
		}
	})
	t.Run("monday_range", func(t *testing.T) {
		got := formatDailyDMText("2026-05-02", "2026-05-04", "weekend body")
		for _, want := range []string{"2026-05-02", "2026-05-04", "~", "weekend body"} {
			if !containsString(got, want) {
				t.Errorf("result %q missing %q", got, want)
			}
		}
	})
}

func TestTitleizeKey(t *testing.T) {
	t.Parallel()
	// Why: titleizeKey is byte-sliced (k[:1]) and intended for ASCII snake/camel
	// keys per its godoc; multi-byte input is out of scope.
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"customer", "Customer"},
		{"customer_name", "Customer_name"},
		{"X", "X"},
	}
	for _, tt := range tests {
		if got := titleizeKey(tt.in); got != tt.want {
			t.Errorf("titleizeKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStringifyJSONValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"integer float", float64(7), "7"},
		{"non-integer float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"slice via marshal", []string{"a", "b"}, `["a","b"]`},
		{"map via marshal", map[string]int{"x": 1}, `{"x":1}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stringifyJSONValue(tt.in); got != tt.want {
				t.Errorf("stringifyJSONValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractJSONKeyOrder(t *testing.T) {
	t.Parallel()
	raw := `[{"customer": "X", "count": 3, "customer": "Y"}, {"region": "EU"}]`
	got := extractJSONKeyOrder(raw)
	want := []string{"customer", "count", "region"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("order[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestOrderItemKeys(t *testing.T) {
	t.Parallel()
	item := map[string]any{"count": 1, "customer": "X", "extra": true}
	preferred := []string{"customer", "count", "missing"}
	got := orderItemKeys(item, preferred)
	want := []string{"customer", "count", "extra"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("keys[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestChunkByParagraph(t *testing.T) {
	t.Parallel()
	t.Run("under limit returns single chunk", func(t *testing.T) {
		t.Parallel()
		got := chunkByParagraph("hello world", 100)
		if len(got) != 1 || got[0] != "hello world" {
			t.Errorf("got %v, want single 'hello world'", got)
		}
	})
	t.Run("splits at paragraph boundary", func(t *testing.T) {
		t.Parallel()
		text := "para one body" + "\n\n" + "para two body" + "\n\n" + "para three body"
		got := chunkByParagraph(text, 20)
		if len(got) < 2 {
			t.Fatalf("expected ≥2 chunks for limit 20, got %d: %v", len(got), got)
		}
		joined := strings.Join(got, "\n\n")
		for _, frag := range []string{"para one body", "para two body", "para three body"} {
			if !strings.Contains(joined, frag) {
				t.Errorf("chunked output lost %q", frag)
			}
		}
	})
}

func TestAppendMrkdwnSections(t *testing.T) {
	t.Parallel()
	t.Run("empty returns input unchanged", func(t *testing.T) {
		t.Parallel()
		base := []slack.Block{}
		got := appendMrkdwnSections(base, "   \n  ")
		if len(got) != 0 {
			t.Errorf("blank text should append nothing, got %d", len(got))
		}
	})
	t.Run("section header promotes to header block", func(t *testing.T) {
		t.Parallel()
		got := appendMrkdwnSections(nil, "intro\n\n## [Overview]\nbody one\n\n## [Insights]\nbody two")
		if len(got) < 4 {
			t.Fatalf("expected ≥4 blocks (intro + headers + bodies), got %d", len(got))
		}
		var headers int
		for _, b := range got {
			if _, ok := b.(*slack.HeaderBlock); ok {
				headers++
			}
		}
		if headers != 2 {
			t.Errorf("expected 2 header blocks, got %d", headers)
		}
	})
	t.Run("no headers passes through as section", func(t *testing.T) {
		t.Parallel()
		got := appendMrkdwnSections(nil, "just a paragraph")
		if len(got) != 1 {
			t.Fatalf("expected 1 block for plain text, got %d", len(got))
		}
	})
}

func TestAppendJSONArrayBlocks(t *testing.T) {
	t.Parallel()
	t.Run("invalid JSON falls back to code block", func(t *testing.T) {
		t.Parallel()
		got := appendJSONArrayBlocks(nil, "not json")
		if len(got) == 0 {
			t.Fatal("fallback should append at least one block")
		}
	})
	t.Run("activity array renders as table section (no dividers)", func(t *testing.T) {
		t.Parallel()
		raw := `[{"customer":"BNI","count":16},{"customer":"Netciti","count":7}]`
		got := appendJSONArrayBlocks(nil, raw)
		if len(got) == 0 {
			t.Fatal("expected at least one block")
		}
		for _, b := range got {
			if _, ok := b.(*slack.DividerBlock); ok {
				t.Error("activity table must not contain divider blocks")
			}
		}
	})
	t.Run("long values promoted to body sections", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("x", 120)
		raw := `[{"detail":"` + long + `","short":"ok"}]`
		got := appendJSONArrayBlocks(nil, raw)
		if len(got) < 2 {
			t.Fatalf("expected fields + body section + divider, got %d", len(got))
		}
	})
}

func TestFormatDailyDMBlocks(t *testing.T) {
	t.Parallel()
	summary := "intro line\n\n## [Activity]\n```json\n[{\"customer\":\"BNI\",\"count\":3}]\n```\n\nclosing"
	blocks := formatDailyDMBlocks("2026-05-02", "2026-05-04", summary)
	if len(blocks) == 0 {
		t.Fatal("no blocks produced")
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); !ok {
		t.Errorf("first block must be header, got %T", blocks[0])
	}
	// Why: range form should embed both dates in the title.
	hb, _ := blocks[0].(*slack.HeaderBlock)
	if !strings.Contains(hb.Text.Text, "2026-05-02") || !strings.Contains(hb.Text.Text, "2026-05-04") {
		t.Errorf("header missing range dates: %q", hb.Text.Text)
	}
}
