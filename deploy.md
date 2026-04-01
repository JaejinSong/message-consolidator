# 배포 가이드 (Deployment Guide) - Stage 2 Decoupling

본 문서는 Caddy와 Go API를 분리한 다중 컨테이너 기반의 배포 및 사전 검증 절차를 설명합니다.

## 1. 사전 검증 (Pre-verification)

배포 전 아래 명령어를 통해 시스템의 안정성을 검증합니다.

```bash
# 백엔드 및 프론트엔드 테스트
npm test && go test ./...

# AI 건전성(Regression) 테스트 및 DB 진단 (mc-util)
go test -tags regression ./ai/... ./tests/regression/...
go run cmd/mc-util/*.go db-diag
```

## 2. 배포 절차 (Docker & Caddy)

### 단계 1: Docker 이미지 빌드 및 푸시
프론트엔드(Caddy)와 백엔드(Go) 이미지를 각각 빌드하여 Artifact Registry에 푸시합니다.

```bash
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# 프론트엔드 빌드 (Vite + Caddy)
docker build -t us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/frontend:latest -f docker/frontend/Dockerfile .

# 백엔드 빌드 (Go API)
docker build -t us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/backend:latest -f docker/backend/Dockerfile .

# 이미지 푸시
docker push us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/frontend:latest
docker push us-central1-docker.pkg.dev/gemini-enterprise-487906/message-consolidator-repo/backend:latest
```

### 단계 2: 설정 파일 업로드 (GCS)
`Caddyfile`이 포함되었는지 확인하십시오.

```bash
gcloud storage cp .env docker-compose.yml Caddyfile gs://message-consolidator-deploy-gemini-enterprise-487906/vps/
```

### 단계 3: VPS 서버 배포
VPS에 접속하여 기존 단일 컨테이너를 제거하고 다중 컨테이너 환경을 구동합니다.

```bash
gcloud compute ssh chat-analyzer-vps --zone=us-central1-a --command="
  cd ~/message-consolidator && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/.env . && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/docker-compose.yml . && 
  gcloud storage cp gs://message-consolidator-deploy-gemini-enterprise-487906/vps/Caddyfile . && 
  
  # 포트 충돌 방지를 위한 Host Nginx 중지
  sudo systemctl stop nginx || true
  
  sudo docker compose pull && sudo docker compose up -d --remove-orphans
"
```

## 3. 사후 검증
배포 후 `https://34.67.133.18.nip.io/health` 엔드포인트가 `OK`를 반환하는지 확인합니다. Caddy의 최초 HTTPS 인증서 발급에 최대 2분 정도 소요될 수 있습니다.
