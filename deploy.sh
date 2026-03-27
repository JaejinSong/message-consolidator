#!/bin/bash
set -e
set -o pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration (Updated step numbers)
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
IMAGE_NAME="app"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"
BUCKET_NAME="message-consolidator-deploy-gemini-enterprise-487906"

# 0. Pre-deployment verification
echo -e "${BLUE}==> Step 0: Running all tests in parallel (Go, UI, AI)...${NC}"

# Load env and export for subshells
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

(go test ./... -v > go_test.log 2>&1) &
GO_PID=$!
(npm test > npm_test.log 2>&1) &
NPM_PID=$!
(go test ./tests/regression -v > ai_test.log 2>&1) &
AI_PID=$!
(node tests/verify-loading-ui.cjs > loading_ui_test.log 2>&1) &
LOADING_PID=$!

echo "Waiting for tests: Go ($GO_PID), UI ($NPM_PID), AI ($AI_PID), Loading ($LOADING_PID)..."
wait $GO_PID || { echo -e "${RED}❌ Go tests failed! Check go_test.log${NC}"; exit 1; }
wait $NPM_PID || { echo -e "${RED}❌ Frontend tests failed! Check npm_test.log${NC}"; exit 1; }
wait $AI_PID || { echo -e "${RED}❌ AI Regression failed! Check ai_test.log${NC}"; exit 1; }
wait $LOADING_PID || { echo -e "${RED}❌ Loading UI verification failed! Check loading_ui_test.log${NC}"; exit 1; }

echo -e "${GREEN}✅ All tests passed!${NC}"

# 1. Frontend Optimization (PurgeCSS)
echo -e "${BLUE}==> Step 1: Optimizing CSS (PurgeCSS)...${NC}"
npm run build:css || { echo -e "${RED}❌ PurgeCSS failed!${NC}"; exit 1; }

# 2. Build and Push
echo -e "${BLUE}==> Step 2: Building and pushing image...${NC}"
gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet
docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest .
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest

# 3. Upload config
echo -e "${BLUE}==> Step 3: Uploading configs...${NC}"
gcloud storage cp .env docker-compose.yml gs://${BUCKET_NAME}/vps/ --project=${PROJECT_ID}

# 4. Deploy to VPS
echo -e "${BLUE}==> Step 4: Restarting container on VPS...${NC}"
gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="
  mkdir -p ~/message-consolidator && cd ~/message-consolidator && 
  gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet &&
  gcloud storage cp gs://${BUCKET_NAME}/vps/.env . && 
  gcloud storage cp gs://${BUCKET_NAME}/vps/docker-compose.yml . && 
  sudo docker-compose pull && sudo docker-compose up -d
"

# 5. Verification
echo -e "${BLUE}==> Step 5: Verifying deployment...${NC}"
echo "Waiting for 'Startup Complete' log (Max 30s)..."

MAX_RETRIES=15
RETRY_COUNT=0
IS_READY=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  if gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --command="sudo docker logs message-consolidator --tail 100 | grep 'Startup Complete'" > /dev/null 2>&1; then
    IS_READY=1
    break
  fi
  echo -n "."
  sleep 2
  RETRY_COUNT=$((RETRY_COUNT+1))
done

if [ $IS_READY -eq 1 ]; then
  echo -e "\n${GREEN}✅ Startup Complete log found!${NC}"
  echo "Giving service 5s to stabilize background connections..."
  sleep 5
else
  echo -e "\n${RED}❌ Timeout: Startup Complete log not found.${NC}"
  gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --command="sudo docker logs message-consolidator --tail 20"
  exit 1
fi

# Multi-stage Health Check
echo -e "${BLUE}==> Checking API status...${NC}"
# Use public /health endpoint which returns "OK"
HEALTH_CHECK_URL="https://34.67.133.18.nip.io/health"

# Try external first
if curl -s -k "$HEALTH_CHECK_URL" | grep -q "OK"; then
  echo -e "${GREEN}✅ External API is healthy!${NC}"
else
  echo -e "${RED}⚠️ External health check failed. Trying internal...${NC}"
  if gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --command="curl -s http://localhost:8080/health" | grep -q "OK"; then
    echo -e "${GREEN}✅ Internal API is healthy! (External might be DNS or Propagation delay)${NC}"
  else
    echo -e "${RED}❌ Both health checks failed! Check logs.${NC}"
    exit 1
  fi
fi

echo -e "${GREEN}🚀 Deployment Successful!${NC}"
