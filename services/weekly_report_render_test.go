package services

import (
	"strings"
	"testing"
)

// These tests characterize the current weekly-report markdown renderer so the
// complexity refactor of mdToEmailHTML can be shown to preserve behavior.

func TestMdToEmailHTML_BlockElements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		md   string
		want []string
	}{
		{
			name: "heading is escaped and styled",
			md:   "## Week <1> & 2",
			want: []string{`<h2 style="color:#1a73e8;margin-top:28px;font-size:16px">Week &lt;1&gt; &amp; 2</h2>`},
		},
		{
			name: "consecutive bullets share one list",
			md:   "- alpha\n- beta",
			want: []string{`<ul style="padding-left:20px">`, "<li style=\"margin:4px 0\">alpha</li>", "<li style=\"margin:4px 0\">beta</li>", "</ul>"},
		},
		{
			name: "horizontal rule",
			md:   "---",
			want: []string{`<hr style="border:none;border-top:1px solid #ddd;margin:16px 0">`},
		},
		{
			name: "plain line becomes a paragraph with inline bold",
			md:   "hello **world**",
			want: []string{`<p style="margin:8px 0;line-height:1.6">hello <strong>world</strong></p>`},
		},
		{
			name: "non-JSON fence renders as pre",
			md:   "```\nplain <text>\n```",
			want: []string{"<pre style=", "plain &lt;text&gt;", "</pre>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mdToEmailHTML(tc.md)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("output missing %q\ngot: %s", w, got)
				}
			}
		})
	}
}

// A list must be closed by any following block element, not left hanging.
func TestMdToEmailHTML_ListClosedByFollowingBlocks(t *testing.T) {
	t.Parallel()
	for _, next := range []string{"## Heading", "---", "", "paragraph", "```\nx\n```"} {
		got := mdToEmailHTML("- item\n" + next)
		if strings.Count(got, "<ul") != strings.Count(got, "</ul>") {
			t.Errorf("unbalanced list for follower %q: %s", next, got)
		}
		if !strings.Contains(got, "</ul>") {
			t.Errorf("list not closed by follower %q: %s", next, got)
		}
	}
}

// Fenced JSON is rendered as a table rather than a code block.
func TestMdToEmailHTML_JSONFenceBecomesTable(t *testing.T) {
	t.Parallel()
	got := mdToEmailHTML("```\n[{\"task\":\"ship\",\"owner\":\"jin\"}]\n```")
	if !strings.Contains(got, "<table") {
		t.Fatalf("expected a table for JSON fence, got: %s", got)
	}
	if strings.Contains(got, "<pre") {
		t.Errorf("JSON fence must not also render a pre block: %s", got)
	}
	for _, want := range []string{">Task</th>", ">Owner</th>", ">ship</td>", ">jin</td>"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q\ngot: %s", want, got)
		}
	}
}

// Markdown inside a fence must survive verbatim instead of being parsed.
func TestMdToEmailHTML_FenceContentNotParsed(t *testing.T) {
	t.Parallel()
	got := mdToEmailHTML("```\n## not a heading\n- not a bullet\n```")
	if strings.Contains(got, "<h2") || strings.Contains(got, "<li") {
		t.Errorf("fence content was parsed as markdown: %s", got)
	}
	if !strings.Contains(got, "## not a heading") {
		t.Errorf("fence content lost: %s", got)
	}
}

// Documents existing behavior for an unterminated fence: the body is dropped and a bare
// closing </pre> is emitted. Locked in so the refactor cannot change it silently.
func TestMdToEmailHTML_UnterminatedFence(t *testing.T) {
	t.Parallel()
	got := mdToEmailHTML("```\ndangling body")
	if !strings.HasSuffix(got, "</pre>\n") {
		t.Errorf("expected trailing </pre>, got: %q", got)
	}
	if strings.Contains(got, "dangling body") {
		t.Errorf("unterminated fence currently drops its body; got: %q", got)
	}
}

func TestJSONToTable(t *testing.T) {
	t.Parallel()
	t.Run("empty for non-JSON", func(t *testing.T) {
		t.Parallel()
		if got := jsonToTable("not json"); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})
	t.Run("empty for JSON array with no rows", func(t *testing.T) {
		t.Parallel()
		if got := jsonToTable("[]"); got != "" {
			t.Errorf("want empty, got %q", got)
		}
	})
	t.Run("escapes cell values", func(t *testing.T) {
		t.Parallel()
		got := jsonToTable(`[{"task":"<script>"}]`)
		if strings.Contains(got, "<script>") {
			t.Errorf("cell value not escaped: %s", got)
		}
		if !strings.Contains(got, "&lt;script&gt;") {
			t.Errorf("want escaped value, got: %s", got)
		}
	})
	t.Run("alternating row background", func(t *testing.T) {
		t.Parallel()
		got := jsonToTable(`[{"a":"1"},{"a":"2"}]`)
		if !strings.Contains(got, `background:#fff`) || !strings.Contains(got, `background:#f8f9fa`) {
			t.Errorf("want alternating row colors, got: %s", got)
		}
	})
}

// titleWords replaces the deprecated strings.Title; these pin the behavior that
// motivated a local helper over x/text (all-caps words keep their casing).
func TestTitleWords(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"task", "Task"},
		{"task_count", "Task_Count"},
		{"due date", "Due Date"},
		{"ID", "ID"},
		{"", ""},
		{"7days", "7Days"},
	}
	for _, tc := range cases {
		if got := titleWords(tc.in); got != tc.want {
			t.Errorf("titleWords(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
