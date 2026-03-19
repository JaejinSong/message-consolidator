---
name: log
description: VPS의 애플리케이션 로그를 확인합니다.
---

// turbo-all
1. 실시간 로그 확인 (Follow)
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="docker logs -f message-consolidator_app_1"
```

2. 최근 100줄 로그 확인
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="docker logs --tail 100 message-consolidator_app_1"
```

3. 에러 로그만 확인 (최근 100줄)
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="docker logs --tail 100 message-consolidator_app_1 2>&1 | grep -i error"
```
