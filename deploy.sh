#!/bin/bash
set -e
set -o pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
IMAGE_NAME="app"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"
BUCKET_NAME="message-consolidator-deploy-gemini-enterprise-487906"

# Helper function to run a step and show progress
run_step() {
    local name="$1"
    shift
    echo -ne "[ RUN ] $name... "
    local start_time=$(date +%s)
    
    local tmp_log=$(mktemp)
    if "$@" > "$tmp_log" 2>&1; then
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        echo -e "\r[${GREEN} PASS ${NC}] $name (${duration}s)"
        rm -f "$tmp_log"
    else
        echo -e "\r[${RED} FAIL ${NC}] $name"
        echo -e "--------------------------------------------------------------------------------"
        cat "$tmp_log"
        echo -e "--------------------------------------------------------------------------------"
        rm -f "$tmp_log"
        exit 1
    fi
}

# 0. Pre-deployment verification
echo -e "${BLUE}==> Step 0: Running pre-deployment tests...${NC}"

# Load env and export for subshells
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

run_step "Go unit tests" go test ./...
run_step "NPM (Vitest) tests" npm test
run_step "AI Regression tests" go test ./tests/regression
run_step "Loading UI verification" node tests/verify-loading-ui.cjs

# 1. Frontend Optimization (PurgeCSS)
echo -e "${BLUE}==> Step 1: Optimizing frontend...${NC}"
run_step "Optimizing CSS (PurgeCSS)" npm run build:css

# 2. Build and Push
echo -e "${BLUE}==> Step 2: Building and pushing image...${NC}"
run_step "GCloud Docker Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet
run_step "Building Docker image" docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest .
run_step "Pushing Docker image" docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest

# 3. Upload config
echo -e "${BLUE}==> Step 3: Uploading configs...${NC}"
run_step "Config transmission to GCS" gcloud storage cp .env docker-compose.yml gs://${BUCKET_NAME}/vps/ --project=${PROJECT_ID}

# 4. Deploy to VPS
echo -e "${BLUE}==> Step 4: VPS Command Execution...${NC}"
run_step "Remote Restart on VPS" gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command="
  mkdir -p ~/message-consolidator && cd ~/message-consolidator && 
  gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet &&
  gcloud storage cp gs://${BUCKET_NAME}/vps/.env . && 
  gcloud storage cp gs://${BUCKET_NAME}/vps/docker-compose.yml . && 
  sudo docker-compose pull && sudo docker-compose up -d
"

# 5. Verification
echo -e "${BLUE}==> Step 5: Verifying deployment...${NC}"
echo "Waiting for 'Startup Complete' log... "

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
  echo "Stabilizing dynamic connections... (5s)"
  sleep 5
else
  echo -e "\n${RED}❌ Timeout: Startup Complete log not found.${NC}"
  gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --command="sudo docker logs message-consolidator --tail 20"
  exit 1
fi

# Multi-stage Health Check
HEALTH_CHECK_URL="https://34.67.133.18.nip.io/health"

run_step "External Health Check" curl -s -k "$HEALTH_CHECK_URL" | grep -q "OK"

echo -e "\n${GREEN}🚀 Deployment Successful!${NC}"
