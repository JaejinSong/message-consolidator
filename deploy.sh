#!/bin/bash
set -e

# Configuration
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
IMAGE_NAME="app"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"
BUCKET_NAME="message-consolidator-deploy-gemini-enterprise-487906"

# 0. 로컬 사전 검증 (Local Pre-verification)
echo "==> Step 0: Local Pre-verification..."

echo "--> 0.1: Tidying Go modules..."
go mod tidy

echo "--> 0.2: Building Go project..."
go build ./...

echo "--> 0.3 & 0.4: Running Tests in Parallel (Go & Frontend)..."
go test ./... &
GO_PID=$!

npm test &
NPM_PID=$!

FAIL=0
wait $GO_PID || { echo "❌ Go backend tests failed!"; FAIL=1; }
wait $NPM_PID || { echo "❌ Frontend tests failed!"; FAIL=1; }

if [ $FAIL -ne 0 ]; then
    echo "❌ Tests failed! Deployment aborted."
    exit 1
fi

echo "✅ Local pre-verification passed!"

# 1. 로컬에서 Docker 이미지 빌드 및 푸시
echo "==> Step 1: Building and pushing Docker image..."
gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet
docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest .
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest

echo "==> Step 2: Transferring config files directly to VPS (Skipping GCS)..."
gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="mkdir -p ~/message-consolidator"
gcloud compute scp .env docker-compose.yml ${VPS_NAME}:~/message-consolidator/ --zone=${ZONE} --project=${PROJECT_ID}

# 3. VPS 배포 및 실시간 검증 (SSH 접속 1회로 통합)
echo "==> Step 3 & 4: Deploying and Verifying on VPS..."
gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="
  cd ~/message-consolidator && 
  gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet &&
  sudo docker-compose pull &&
  sudo docker-compose up -d &&
  echo 'Waiting for Startup Complete log...' &&
  sudo docker-compose logs --tail=20 | grep -q \"Startup Complete\" || (sleep 5 && sudo docker-compose logs --tail=20 | grep \"Startup Complete\") &&
  echo 'Checking local health (localhost:8080)...' &&
  curl -s -f http://localhost:8080/health && echo 'Local health check passed!'
"

# 외부 API 상태 확인 (Public IP를 통한 최종 검증)
echo "==> Checking External API status..."
# Wait a few seconds for the proxy/network to propagate if needed
sleep 2
EXTERNAL_IP=$(gcloud compute instances describe ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --format="value(networkInterfaces[0].accessConfigs[0].natIP)")
echo "External IP: ${EXTERNAL_IP}"

# Try both nip.io and direct IP
if curl -s -f "https://${EXTERNAL_IP}.nip.io/health"; then
    echo "✅ API is healthy via HTTPS (nip.io)!"
elif curl -s -f "http://${EXTERNAL_IP}:8080/health"; then
    echo "⚠️ API is healthy via HTTP (Direct Port), but HTTPS/nip.io failed."
else
    echo "❌ API health check failed on all endpoints!"
    exit 1
fi
