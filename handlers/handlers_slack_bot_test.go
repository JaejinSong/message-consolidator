package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestSlackRetryHeader(t *testing.T) {
	cases := []struct {
		name       string
		num        string
		reason     string
		wantNum    string
		wantReason string
		wantSkip   bool
	}{
		{name: "no retry header", num: "", reason: "", wantSkip: false},
		{name: "first delivery (num=0)", num: "0", reason: "", wantSkip: false},
		{name: "first retry", num: "1", reason: "http_timeout", wantNum: "1", wantReason: "http_timeout", wantSkip: true},
		{name: "later retry without reason", num: "3", reason: "", wantNum: "3", wantReason: "", wantSkip: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/slack/events", nil)
			if tc.num != "" {
				r.Header.Set("X-Slack-Retry-Num", tc.num)
			}
			if tc.reason != "" {
				r.Header.Set("X-Slack-Retry-Reason", tc.reason)
			}
			num, reason, skip := slackRetryHeader(r)
			if skip != tc.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tc.wantSkip)
			}
			if num != tc.wantNum {
				t.Errorf("num = %q, want %q", num, tc.wantNum)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
