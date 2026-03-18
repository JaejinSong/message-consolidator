---
description: VPS 배포 워크플로우 (GCP VPS 자동 배포)
---

// turbo-all
1. 로컬에서 바이너리 빌드 (Linux/AMD64)
```bash
go mod tidy
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o message-consolidator-vps .
```

2. VPS로 파일 및 정적 자산 전송
```bash
gcloud compute scp .env docker-compose.yml Dockerfile.runtime go.mod go.sum message-consolidator-vps chat-analyzer-vps:~/message-consolidator/ --zone=us-central1-a --project=gemini-enterprise-487906
gcloud compute scp --recurse static chat-analyzer-vps:~/message-consolidator/ --zone=us-central1-a --project=gemini-enterprise-487906
```

3. VPS에서 컨테이너 재시작 및 배포 완료
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="cd ~/message-consolidator && cp Dockerfile.runtime Dockerfile && docker-compose down && docker-compose up -d"
```

4. VPS 배포 상태 및 실시간 검증
- **검증 주소**: https://34.67.133.18.nip.io/
- **확인 항목**:
    1. 메인 화면 로드 확인
    2. `/api/scan?lang=Korean` 호출 후 `scan started` 응답 확인 (브라우저 sub-agent 사용 권장)
