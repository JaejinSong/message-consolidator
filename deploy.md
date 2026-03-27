# 배포 가이드 (Deployment Guide)

본 문서는 `message-consolidator` 프로젝트의 배포 및 사전 검증 절차를 설명합니다.

## 1. 사전 검증 (Pre-verification)

배포 전 아래 명령어를 통해 시스템의 안정성을 검증합니다.

```bash
# 백엔드 및 프론트엔드 테스트
npm test && go test ./...

# AI 건전성(Regression) 테스트 및 DB 진단 (mc-util)
go test ./tests/regression -v
go run cmd/mc-util/*.go db-diag
```

### AI 건전성 테스트 설정
AI 건전성 테스트에는 Gemini API 키가 필요합니다. 아래 두 가지 방법 중 하나로 설정할 수 있습니다.
1. **환경 변수**: `export GEMINI_API_KEY_FOR_TEST="your_key"`
2. **.env 파일**: 프로젝트 루트의 `.env` 파일에 `GEMINI_API_KEY_FOR_TEST=your_key` 추가 (추천)

## 2. 배포 절차 (/deploy 워크플로우)

### 단계 1: Docker 이미지 빌드 및 푸시
```bash
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet
docker build -t us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/app:latest .
docker push us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/app:latest
```

### 단계 2: 설정 파일 업로드 (GCS)
```bash
gcloud storage cp .env docker-compose.yml gs://message-consolidator-deploy-gemini-enterprise-487906/vps/
```

### 단계 3: VPS 서버 배포
VPS에 접속하여 최신 이미지를 반영합니다.
```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --command="
  cd ~/message-consolidator && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/.env . && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/docker-compose.yml . && 
  sudo docker-compose pull && sudo docker-compose up -d
"
```

## 3. 사후 검증
배포 후 `https://34.67.133.18.nip.io/health` 엔드포인트가 `OK`를 반환하는지 확인합니다.
