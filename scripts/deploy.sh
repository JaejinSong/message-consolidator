#!/bin/bash

# Configuration
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
IMAGE_NAME="app"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"
BUCKET_NAME="message-consolidator-deploy-gemini-enterprise-487906"

# 1. 로컬에서 Docker 이미지 빌드 및 푸시
echo "==> Step 1: Building and pushing Docker image..."
gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet
docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest .
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest

# 2. 설정 파일(docker-compose.yml, .env)을 GCS에 업로드
echo "==> Step 2: Uploading config files to GCS..."
gcloud storage cp .env docker-compose.yml gs://${BUCKET_NAME}/vps/ --project=${PROJECT_ID}

# 3. VPS에서 이미지 Pull 및 컨테이너 재시작
echo "==> Step 3: Restarting container on VPS..."
gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="
  mkdir -p ~/message-consolidator && 
  cd ~/message-consolidator && 
  gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet &&
  gcloud storage cp gs://${BUCKET_NAME}/vps/.env . && 
  gcloud storage cp gs://${BUCKET_NAME}/vps/docker-compose.yml . && 
  sudo docker-compose pull && 
  sudo docker-compose up -d
"

# 4. VPS 배포 상태 및 실시간 검증
echo "==> Step 4: Verifying deployment..."
# 로그 확인 (Startup Complete 문구가 보일 때까지 대기 또는 1회 확인)
gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="
  cd ~/message-consolidator && 
  sudo docker-compose logs | grep \"Startup Complete\"
"

# API 상태 확인
echo "==> Checking API status..."
curl -s -X GET "https://34.67.133.18.nip.io/api/scan?lang=Korean" | grep -q "scan started" && echo "API is healthy!" || echo "API health check failed!"
