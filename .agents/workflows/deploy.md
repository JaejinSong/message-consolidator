---
name: deploy
description: VPS 배포 워크플로우 (Google Artifact Registry 활용)
---

// turbo-all
# Size Optimization: Binary stripped with -ldflags="-s -w" and compressed with upx (~37MB -> ~10MB)
1. 로컬에서 Docker 이미지 빌드 및 푸시
```bash
# Artifact Registry 인증 (최초 1회 필요)
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# 이미지 빌드 및 푸시
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
  mkdir -p ~/message-consolidator && 
  cd ~/message-consolidator && 
  gcloud auth configure-docker us-central1-docker.pkg.dev --quiet &&
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/.env . && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/docker-compose.yml . && 
  sudo docker-compose pull && 
  sudo docker-compose up -d
"
```

4. VPS 배포 상태 및 실시간 검증
- **검증 주소**: https://34.67.133.18.nip.io/
- **확인 항목**:
    1. 기동 로그 확인
    2. 메인 화면 로드 확인
    . `/api/scan?lang=Korean` 호출 후 `scan started` 응답 확인