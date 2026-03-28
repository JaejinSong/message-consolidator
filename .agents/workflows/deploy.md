---
description: VPS 배포 워크플로우 (Google Artifact Registry 활용)
---

// turbo-all
# Size Optimization: Binary stripped with -ldflags="-s -w" and compressed with upx (~37MB -> ~10MB)

0. 로컬 사전 검증 (Local Pre-verification)
```bash
# 1. 백엔드(Go) 및 프론트엔드(Node) 테스트 병렬 실행 (Faster)
(go test ./... -v > go_test.log 2>&1) &
(npm test > npm_test.log 2>&1) &
wait

# 2. AI 건전성(Regression) 및 DB 진단 (Must Pass)
# GEMINI_API_KEY_FOR_TEST 환경변수 필요
go test ./tests/regression -v
go run cmd/mc-util/*.go db-diag
```

1. 로컬에서 Docker 이미지 빌드 및 푸시
```bash
# Artifact Registry 인증 및 빌드/푸시
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet
docker build -t us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/app:latest .
docker push us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/app:latest
```

2. 설정 파일(docker-compose.yml, .env)을 GCS에 업로드
```bash
gcloud storage cp .env docker-compose.yml gs://message-consolidator-deploy-gemini-enterprise-487906/vps/ --project=gemini-enterprise-487906
```

3. VPS에서 이미지 Pull 및 컨테이너 재설작
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --project=gemini-enterprise-487906 --command="
  mkdir -p ~/message-consolidator && cd ~/message-consolidator && 
  gcloud auth configure-docker us-central1-docker.pkg.dev --quiet &&
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/.env . && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/docker-compose.yml . && 
  sudo docker-compose pull && sudo docker-compose up -d --force-recreate --remove-orphans
"
```

4. VPS 배포 상태 및 실시간 검증
- **로그 및 기동 검증 (Robust)**: 
    1. `Startup Complete` 로그가 나올 때까지 최대 30초간 대기 루프 실행
    2. API 헬스체크: `https://34.67.133.18.nip.io/api/scan?lang=Korean` 호출
    3. `scan started` 응답 확인 시 최종 성공 판정