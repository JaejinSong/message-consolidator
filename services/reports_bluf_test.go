package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"message-consolidator/store"
)

// blufNow anchors every case to the window that produced the observed defect (report 183,
// weekly 2026-08-29 ~ 2026-09-04).
var blufNow = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

type blufFixture struct {
	id        int64
	task      string
	room      string
	source    string
	assignee  string
	requester string
	reqType   string
	createdAt string
	updatedAt string // "" means never updated
	deadline  string
	done      bool
	excluded  bool
	evidence  string
}

func (f blufFixture) toLog() Log {
	m := Log{
		ID:            store.MessageID(f.id),
		Task:          f.task,
		Room:          f.room,
		Source:        f.source,
		Assignee:      f.assignee,
		Requester:     f.requester,
		RequesterType: f.reqType,
		Deadline:      f.deadline,
		Done:          f.done,
		OriginalText:  f.evidence,
	}
	if f.createdAt != "" {
		m.CreatedAt = mustDay(f.createdAt)
		m.AssignedAt = m.CreatedAt
	}
	if f.updatedAt != "" {
		u := mustDay(f.updatedAt)
		m.UpdatedAt = &u
	}
	if f.excluded {
		e := m.CreatedAt
		m.ExcludedAt = &e
	}
	return m
}

func mustDay(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func toLogs(fs []blufFixture) []Log {
	out := make([]Log, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.toLog())
	}
	return out
}

func rankOf(t *testing.T, dossiers []blufDossier, id int64) int {
	t.Helper()
	for i, d := range dossiers {
		if int64(d.log.ID) == id {
			return i + 1
		}
	}
	return -1
}

func summarize(dossiers []blufDossier) string {
	var sb strings.Builder
	for i, d := range dossiers {
		fmt.Fprintf(&sb, "\n  #%d id=%d score=%d %q [%s]", i+1, d.log.ID, d.score, d.log.Task, strings.Join(d.signals, "; "))
	}
	return sb.String()
}

// windowActivity mirrors the real 2026-08-29~09-04 activity rows: four done PDRM tasks give
// the window its dominant subject, and the newest open task is a single-mention Puspakom item.
func windowActivity() []blufFixture {
	return []blufFixture{
		{id: 13190, task: "Prepare POC review report for Puspakom next review", room: "Internal Puspakom WhaTap IFC", source: "whatsapp", assignee: "Andy Phan", requester: "Diana", createdAt: "2026-09-04", updatedAt: "2026-09-04", evidence: "Please prepare the review report before the next Puspakom review."},
		{id: 13173, task: "Investigate MySQL error on Notihub account", room: "Adira - Whatap Tech", source: "whatsapp", assignee: "shared", requester: "Sermon", createdAt: "2026-09-02", updatedAt: "2026-09-02", evidence: "The Notihub account shows a MySQL error we cannot explain."},
		{id: 13138, task: "Enable AI on-premise for Adira by connecting to their LLM", room: "Adira - Whatap Tech", source: "whatsapp", assignee: "Sermon Paskah Zagoto", requester: "Yoga", createdAt: "2026-08-31", updatedAt: "2026-09-01", evidence: "We need the on-prem AI wired to their own LLM endpoint."},
		{id: 13158, task: "Summarize WhaTap Browser Monitoring capabilities for DHAS POC test results", room: "Gmail", source: "gmail", assignee: "Jaejin Song (JJ)", requester: "yspark@whatap.io", createdAt: "2026-09-02", updatedAt: "2026-09-03", evidence: "Summarize what Browser Monitoring covers for the DHAS test."},
		// Done PDRM rows: they carry the window's dominant subject but can never be a BLUF.
		{id: 13184, task: "Issue V1 product license for PDRM Malaysia next-generation system PoC", room: "Gmail", source: "gmail", assignee: "Yoga Wiranda", requester: "Stephen Tok", createdAt: "2026-09-04", updatedAt: "2026-09-04", deadline: "2026-09-03", done: true},
		{id: 13183, task: "Provide collector server IP addresses to proceed with PDRM Malaysia PoC", room: "Gmail", source: "gmail", assignee: "Yoga Wiranda", requester: "Stephen Tok", createdAt: "2026-09-03", updatedAt: "2026-09-03", done: true},
		{id: 13182, task: "Resolve Java path space error to run batch script", room: "PDRM POC - MSB | IFC | WhaTap", source: "whatsapp", assignee: "Faisal", requester: "Khairuz", createdAt: "2026-09-04", updatedAt: "2026-09-04", done: true},
		{id: 13181, task: "Track POC license submission for PDRM and forward request mail", room: "PDRM POC - MSB | IFC | WhaTap", source: "whatsapp", assignee: "Jaejin Song (JJ)", requester: "Stephen Tok", createdAt: "2026-09-04", updatedAt: "2026-09-04", done: true},
	}
}

// windowStalled mirrors the pre-window backlog that Layer 0 made visible: never-updated rows
// that the report query used to drop because updated_at was NULL.
func windowStalled() []blufFixture {
	return []blufFixture{
		{id: 12611, task: "Follow up on contract expiration for FIF SaaS APM Server DB Browser", room: "Gmail", source: "gmail", assignee: "Andy Phan", requester: "billing@fif.co.id", reqType: "external", createdAt: "2026-07-01", evidence: "The FIF SaaS contract expires soon and nobody has confirmed the renewal path."},
		{id: 11867, task: "Manage 2026 stock option exercise and complete payment", room: "Gmail", source: "gmail", assignee: "hostuser@whatap.io", requester: "hr@whatap.io", createdAt: "2026-05-12", evidence: "Exercise window for the 2026 grant closes and payment must be completed."},
		{id: 12440, task: "Identify link and domain for whitelisting AI analysis recommendation", room: "Internal Puspakom WhaTap IFC", source: "whatsapp", assignee: "shared", requester: "Diana", createdAt: "2026-06-18", updatedAt: "2026-08-24", evidence: "Raising this again - the whitelisting domain is still blocking our analysis."},
		{id: 12697, task: "Progress SAP Ariba registration with DPM partner", room: "biz-global-thailand", source: "slack", assignee: "shared", requester: "Golf", createdAt: "2026-07-09", updatedAt: "2026-08-16", evidence: "Ariba registration is still stuck waiting on the partner."},
	}
}

func TestBuildBLUFDossiers_NewestSingleMentionTaskIsNotTop(t *testing.T) {
	dossiers := buildBLUFDossiers(toLogs(windowActivity()), toLogs(windowStalled()), "hostuser@whatap.io", blufNow)
	if len(dossiers) == 0 {
		t.Fatal("expected candidates, got none")
	}
	// Why: report 183 and its same-week daily report 184 both opened on this exact task -- the
	// newest open line, one mention, zero age. It must no longer win by position alone.
	if got := int64(dossiers[0].log.ID); got == 13190 {
		t.Errorf("newest single-mention Puspakom task still ranks #1 (the observed defect)%s", summarize(dossiers))
	}
	if rank := rankOf(t, dossiers, 13190); rank > 0 && rank <= 3 {
		t.Errorf("newest single-mention task ranked #%d, expected outside the top 3%s", rank, summarize(dossiers))
	}
}

func TestBuildBLUFDossiers_SurfacesLongSilentBacklog(t *testing.T) {
	dossiers := buildBLUFDossiers(toLogs(windowActivity()), toLogs(windowStalled()), "hostuser@whatap.io", blufNow)
	// Why: a revenue-bearing follow-up that nobody has touched in 65 days is the canonical
	// "you missed this" item; it used to be unreachable by any report.
	if rank := rankOf(t, dossiers, 12611); rank < 1 || rank > 3 {
		t.Errorf("65-day silent contract-expiration follow-up ranked #%d, want top 3%s", rank, summarize(dossiers))
	}
	if rank := rankOf(t, dossiers, 11867); rank < 1 {
		t.Errorf("115-day self-owned backlog item absent from candidates%s", summarize(dossiers))
	}
}

func TestBuildBLUFDossiers_ResurfacedOutranksNeverTouchedAtSameAge(t *testing.T) {
	base := []blufFixture{
		{id: 1, task: "Confirm Adira staging rollout plan", room: "Adira - Whatap Tech", source: "whatsapp", assignee: "Sermon", requester: "Yoga", createdAt: "2026-07-01"},
		{id: 2, task: "Confirm Adira staging capacity plan", room: "Adira - Whatap Tech", source: "whatsapp", assignee: "Sermon", requester: "Yoga", createdAt: "2026-07-01", updatedAt: "2026-08-28"},
	}
	dossiers := buildBLUFDossiers(toLogs(base), nil, "hostuser@whatap.io", blufNow)
	if int64(dossiers[0].log.ID) != 2 {
		t.Errorf("resurfaced task should outrank the never-touched one at equal age%s", summarize(dossiers))
	}
}

func TestBuildBLUFDossiers_SkipsDoneAndExcluded(t *testing.T) {
	base := []blufFixture{
		{id: 1, task: "Closed Adira migration checkpoint", room: "Adira - Whatap Tech", assignee: "Sermon", createdAt: "2026-06-01", done: true},
		{id: 2, task: "Parked Adira pricing revision", room: "Adira - Whatap Tech", assignee: "Sermon", createdAt: "2026-06-01", excluded: true},
		{id: 3, task: "Open Adira capacity review", room: "Adira - Whatap Tech", assignee: "Sermon", createdAt: "2026-06-01"},
	}
	dossiers := buildBLUFDossiers(toLogs(base), nil, "hostuser@whatap.io", blufNow)
	if len(dossiers) != 1 || int64(dossiers[0].log.ID) != 3 {
		t.Errorf("want only the open non-excluded task as candidate%s", summarize(dossiers))
	}
	// Why: the done task still has to feed the topic index, otherwise closing work on a subject
	// would erase that subject's salience.
	if dossiers[0].mentions < 3 {
		t.Errorf("done and excluded tasks must still count toward topic mentions, got %d", dossiers[0].mentions)
	}
}

func TestBuildBLUFDossiers_DedupsTaskPresentInBothSections(t *testing.T) {
	f := blufFixture{id: 42, task: "Progress SAP Ariba registration with DPM partner", room: "biz-global-thailand", assignee: "shared", createdAt: "2026-07-09", updatedAt: "2026-08-16"}
	dossiers := buildBLUFDossiers(toLogs([]blufFixture{f}), toLogs([]blufFixture{f}), "hostuser@whatap.io", blufNow)
	if len(dossiers) != 1 {
		t.Fatalf("want 1 deduped candidate, got %d%s", len(dossiers), summarize(dossiers))
	}
	if dossiers[0].mentions != 1 {
		t.Errorf("overlapping task inflated topic mentions to %d, want 1", dossiers[0].mentions)
	}
}

func TestBLUFTopicTokens_DropsGenericVerbsAndChannels(t *testing.T) {
	cases := []struct {
		name    string
		fixture blufFixture
		want    []string
		absent  []string
	}{
		{
			name:    "proper nouns survive, sentence-case verb does not",
			fixture: blufFixture{task: "Issue V1 product license for PDRM Malaysia next-generation system PoC", room: "Gmail"},
			want:    []string{"pdrm", "malaysia"},
			absent:  []string{"issue", "product", "license", "poc", "gmail"},
		},
		{
			name:    "room contributes the counterparty once scaffolding is stripped",
			fixture: blufFixture{task: "Resolve Java path space error to run batch script", room: "PDRM POC - MSB | IFC | WhaTap"},
			want:    []string{"pdrm", "java"},
			absent:  []string{"whatap", "poc", "resolve", "script"},
		},
		{
			name:    "generic mailbox room contributes nothing",
			fixture: blufFixture{task: "Review and respond to billing proposal", room: "Gmail"},
			want:    nil,
			absent:  []string{"gmail", "review", "respond", "billing"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := blufTopicTokens(tc.fixture.toLog())
			set := make(map[string]bool, len(got))
			for _, g := range got {
				set[g] = true
			}
			for _, w := range tc.want {
				if !set[w] {
					t.Errorf("missing topic token %q in %v", w, got)
				}
			}
			for _, a := range tc.absent {
				if set[a] {
					t.Errorf("generic token %q leaked into topics %v", a, got)
				}
			}
		})
	}
}

func TestBuildBLUFDossiers_OrderIsDeterministic(t *testing.T) {
	activity, stalled := toLogs(windowActivity()), toLogs(windowStalled())
	first := buildBLUFDossiers(activity, stalled, "hostuser@whatap.io", blufNow)
	for i := 0; i < 20; i++ {
		next := buildBLUFDossiers(activity, stalled, "hostuser@whatap.io", blufNow)
		if len(next) != len(first) {
			t.Fatalf("candidate count drifted: %d vs %d", len(next), len(first))
		}
		for j := range first {
			if first[j].log.ID != next[j].log.ID || first[j].score != next[j].score {
				t.Fatalf("order drifted at #%d: %d(%d) vs %d(%d)", j+1, first[j].log.ID, first[j].score, next[j].log.ID, next[j].score)
			}
		}
	}
}

func TestBuildBLUFCandidateLine_ListsRankedShortlistWithSignals(t *testing.T) {
	line := buildBLUFCandidateLine(toLogs(windowActivity()), toLogs(windowStalled()), "hostuser@whatap.io", blufNow)
	if !strings.HasPrefix(line, "# BLUF candidates (ranked): 1) ") {
		t.Fatalf("unexpected hint line prefix: %q", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Error("hint line must end with a newline so the stats block stays one key per line")
	}
	if strings.Count(line, ") \"") != blufHintCount {
		t.Errorf("want %d ranked entries, got line: %s", blufHintCount, line)
	}
}

func TestIsBLUFCandidate_SkipsMergedRowsThatStillReadOpen(t *testing.T) {
	// Why: a merged row keeps done=0 after its content moved to the surviving task -- reading it
	// as neglect is exactly how a completed stock-option item was misreported as 115 days idle.
	merged := blufFixture{id: 1, task: "Manage stock option exercise", createdAt: "2026-05-12"}.toLog()
	merged.Category = "merged"
	if isBLUFCandidate(merged) {
		t.Error("merged row must not be a BLUF candidate")
	}
	open := blufFixture{id: 2, task: "Manage stock option exercise", createdAt: "2026-05-12"}.toLog()
	open.Category = "TASK"
	if !isBLUFCandidate(open) {
		t.Error("open TASK row must be a candidate")
	}
}

func TestMessageBlocks_CountsAppendedMessages(t *testing.T) {
	cases := map[string]int{
		"":                            0,
		"single message":              1,
		"newest\n\nolder":             2,
		"a\n\nb\n\nc\n\nd":            4,
		"  padded\n\nblocks  \n\n x ": 3,
	}
	for in, want := range cases {
		if got := messageBlocks(in); got != want {
			t.Errorf("messageBlocks(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestBuildBLUFDossiers_RepeatedAskOutranksSingleMessageAtSameAge(t *testing.T) {
	seven := strings.Join([]string{"Any update on the renewal?", "Following up again.", "Still waiting.", "Ping.", "Hello?", "Renewal?", "First ask."}, "\n\n")
	base := []blufFixture{
		{id: 1, task: "Confirm Meridian renewal quote", room: "Gmail", assignee: "Rina", createdAt: "2026-07-01", evidence: "First ask."},
		{id: 2, task: "Confirm Meridian license terms", room: "Gmail", assignee: "Rina", createdAt: "2026-07-01", evidence: seven},
	}
	dossiers := buildBLUFDossiers(toLogs(base), nil, "hostuser@whatap.io", blufNow)
	if int64(dossiers[0].log.ID) != 2 {
		t.Errorf("seven-message thread should outrank a single message at equal age%s", summarize(dossiers))
	}
	if !strings.Contains(strings.Join(dossiers[0].signals, ";"), "thread carries 7 messages") {
		t.Errorf("repeat-ask signal missing from labels: %v", dossiers[0].signals)
	}
}

func TestIsExternalParty_ResolvesByDomainWhenContactTypeIsNone(t *testing.T) {
	host := "hostuser@whatap.io"
	cases := []struct {
		contactType, canonical, raw string
		want                        bool
	}{
		{"none", "billing@fif.co.id", "", true},       // unresolved non-company domain -> External
		{"none", "", "Diana", true},                   // bare display name, no company signal -> External
		{"none", "yspark@whatap.io", "", false},       // company domain -> Internal
		{"customer", "", "Whoever", true},             // stored type wins
		{"partner", "", "Whoever", true},              //
		{"internal", "someone@gmail.com", "", false},  // stored type wins over domain
		{"none", "", "Jaejin Song (Ambiguous)", true}, // ambiguity suffix stripped before mapping
	}
	for _, tc := range cases {
		if got := isExternalParty(tc.contactType, tc.canonical, tc.raw, host); got != tc.want {
			t.Errorf("isExternalParty(%q,%q,%q) = %v, want %v", tc.contactType, tc.canonical, tc.raw, got, tc.want)
		}
	}
}
