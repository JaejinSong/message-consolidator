---
description: VPS 서비스 상태 및 API 정상 동작 검증
---

// turbo-all
1. 스캔 API 검증
- 주소: https://34.67.133.18.nip.io/api/scan?lang=Korean
- 응답 JSON에 `"status": "scan started"`가 포함되어 있는지 확인합니다.

2. 서버 로그 최종 확인
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="docker logs message-consolidator_app_1 --tail 50"
```