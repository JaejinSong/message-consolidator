---
description: Commit Workflow (Optimized)
---

1. `go run scripts/generate_release_notes.go` 실행 후 `RELEASE_NOTES_USER.md` 업데이트 (KR 포함)
2. README.md 업데이트 필요 시 수정
3. 완료 내역이 있다면 TODO.md 에 완료 처리 

// turbo
3. git add . && git commit -m "[feat/fix]: [Description]" && git push