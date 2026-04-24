package ai

// GetDefaultFewShots는 모든 분석기에서 공통으로 사용할 수 있는 기본 예시 풀을 반환합니다.
// Why: [Token Economy] 프롬프트에 하드코딩된 예시를 제거하고, 필요할 때만 동적으로 선택하여 주입함으로써 토큰 절감 및 응답 속도를 개선합니다.
func GetDefaultFewShots() []FewShot {
	return []FewShot{
		{
			Input:    "[2026-03-26 10:00:00] Alice: @Bob please update the API documentation by this Friday.\n[2026-03-26 10:05:00] Bob: I'll do that. I will have it ready.",
			Expected: `[{"id": 1, "state": "new", "task": "Update the API documentation", "requester": "Alice", "assignee": "Bob", "assignee_reason": "Bob explicitly agreed to the request", "deadline": "this Friday", "category": "PROMISE", "source_ts": "2026-03-26 10:05:00"}]`,
		},
		{
			Input:    "[ID:p1] Manager: Can you finish the blog post within this week?\n[ID:p2] Jaejin: Yes, I will finish it by Friday.",
			Expected: `[{"id": 1, "state": "new", "task": "Finish the blog post", "requester": "Manager", "assignee": "Jaejin", "assignee_reason": "Jaejin confirmed the task", "deadline": "Friday", "category": "PROMISE", "source_ts": "p2"}]`,
		},
		{
			Input:    "[ID:Slack_1] User: Please deploy the app.\n[ID:Slack_2] Dev: I'm on it.",
			Expected: `[{"id": 1, "state": "new", "task": "Deploy the app", "requester": "User", "assignee": "Dev", "assignee_reason": "Dev accepted the deployment task", "category": "PROMISE", "source_ts": "Slack_2"}]`,
		},
		{
			Input:    "[ID:wa1] Jhonatan: Pagi, fyi POC report sudah kirim ke pak Sudianto. Mohon dicek ya pak.\n[ID:wa2] Sudianto: Ok Pak saya cek.\n[ID:wa3] Jhonatan: Baik pak trimakasih. Moon feedback kalau ada ya pak.",
			Expected: `[{"id": 1, "state": "new", "task": "Review POC report and provide feedback", "requester": "Jhonatan", "assignee": "Sudianto", "assignee_reason": "Sudianto committed with first-person language 'saya cek' without explicit mention", "category": "PROMISE", "source_ts": "wa2"}]`,
		},
		{
			Input:    "[ID:wa1] Agi: btw sy dpt info masih ada issue collecting data log untuk server yang menggunakan nginx\n[ID:wa2] Agi: tidak selancar apache\n[ID:wa3] Agi: untuk hal ini seperti apa tindak lanjutnya?",
			Expected: `[{"id": 0, "state": "new", "task": "Investigate Nginx data log collection issue", "requester": "Agi", "assignee": "shared", "assignee_reason": "Agi reported a technical issue to the group without directing it at a specific person; no one explicitly committed to handle it", "category": "QUERY", "source_ts": "wa3"}]`,
		},
		{
			Input:    "[ID:wa1] Kamal: nnti kalo ada tambahan di request langsung aja pak sama mas hady.",
			Expected: `[{"id": 0, "state": "new", "task": "Contact Hady for additional project or license requests", "requester": "Kamal", "assignee": "shared", "assignee_reason": "Kamal broadcast a policy directive to the group; not directed at a specific person", "category": "POLICY", "source_ts": "wa1"}]`,
		},
		{
			Input:    "[ID:sl1] JJ: Raise the request to the dev team once Yoga provides business context, while addressing the scalability concerns regarding manual case verification.",
			Expected: `[{"id": 0, "state": "new", "task": "Raise scalability concern to the dev team once business context is provided", "requester": "JJ", "assignee": "Yoga", "assignee_reason": "JJ is waiting on Yoga to provide business context before proceeding; Yoga must act first", "category": "WAITING", "source_ts": "sl1"}]`,
		},
	}
}
