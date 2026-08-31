package core

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
			Input:    "[ID:wa1] Sunpho: ok thankyou pak Hady, besok saya infokan lagi ya pak Hady\n[ID:wa2] Sunpho: sambil saya minta pak Handi untuk share capturean nya ya",
			Expected: `[{"id": 0, "state": "new", "task": "Investigate the issue and report findings tomorrow", "requester": "Sunpho", "assignee": "Sunpho", "assignee_reason": "Sunpho explicitly committed first-person follow-up ('saya infokan lagi') for their own action; requester == assignee when the speaker promises self-driven follow-up, not a group broadcast", "category": "PROMISE", "source_ts": "wa1"}]`,
		},
		{
			Input:    "[ID:wa1] Laurence: ok\n[ID:wa2] Laurence: i will prepare then alignment scope and timeline\n[Document: poc-cases.pptx]",
			Expected: `[{"id": 0, "state": "new", "task": "Prepare alignment scope and timeline for the POC", "requester": "Laurence", "assignee": "Laurence", "assignee_reason": "Laurence explicitly committed first-person ('i will prepare') to drive the prep themselves; an attached document does not override an explicit self-commit by the sender", "category": "PROMISE", "source_ts": "wa2"}]`,
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


// GetDefaultGmailFewShots returns the immutable Gmail seed pool.
// Why: Korean FYI-style share mails ("공유합니다", forwarded reports, group notices)
// were the dominant false-positive source -- reference-worthy mails extracted as
// personal tasks. The seeds teach the share-vs-request boundary; learned examples
// are appended on top and never mutate these.
func GetDefaultGmailFewShots() []FewShot {
	return []FewShot{
		{
			Input:    "T: \"송재진\" <jjsong@whatap.io>\nC: \"박요셉\" <yspark@whatap.io>\nS: FW: [고객사] POC 결과 정리\nB:\n안녕하세요.\n지난주 진행한 POC 결과 자료 공유합니다. 참고 부탁드립니다.\n감사합니다.",
			Expected: `[]`,
			Source:   "gmail",
			Lang:     "ko",
		},
		{
			Input:    "T: Dongin Lee <dilee@whatap.io>, \"송재진\" <jjsong@whatap.io>\nC: \nS: 8월 태국 출장 보고 공유\nB:\n8월 17-21 태국 출장 보고서 전달드립니다.\n문의사항 있으시면 말씀 주세요.",
			Expected: `[]`,
			Source:   "gmail",
			Lang:     "ko",
		},
		{
			Input:    "T: \"송재진\" <jjsong@whatap.io>\nC: \"김남석\" <nskim@whatap.io>\nS: 견적서 검토 요청\nB:\n재진님, 첨부한 견적서 검토 후 금요일까지 회신 부탁드립니다.",
			Expected: `[{"id": 0, "state": "new", "task": "Review the attached quotation and reply", "requester": "", "assignee": "송재진", "assignee_reason": "the mail directly asks the To recipient to review and reply; a share-only mail would request nothing", "deadline": "금요일", "category": "TASK", "source_ts": ""}]`,
			Source:   "gmail",
			Lang:     "ko",
		},
	}
}
