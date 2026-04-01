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
IMAGE_FE="frontend"
IMAGE_BE="backend"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"
BUCKET_NAME="message-consolidator-deploy-gemini-enterprise-487906"

# Mode: all (default), fe, be
MODE=${1:-all}

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

deploy_fe() {
    echo -e "${BLUE}==> Frontend: Optimizing and Building...${NC}"
    run_step "FE: Optimizing CSS" npm run optimize:css
    run_step "FE: CSS Integrity" node verify-css.cjs
    run_step "FE: Building image" docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest -f docker/frontend/Dockerfile .
    run_step "FE: Pushing image" docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest
}

deploy_be() {
    echo -e "${BLUE}==> Backend: Building...${NC}"
    run_step "BE: Building image" docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest -f docker/backend/Dockerfile .
    run_step "BE: Pushing image" docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest
}

# 0. Pre-deployment verification
echo -e "${BLUE}==> Step 0: Running pre-deployment tests (MODE: $MODE)...${NC}"
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

# Why: Only run relevant tests per mode to speed up selective deployment.
if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then
    run_step "Go unit tests" go test ./...
    run_step "AI Regression tests" go test -tags regression ./ai/... ./tests/regression/...
fi

if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then
    run_step "NPM (Vitest) tests" npm test
fi

# Docker Auth
run_step "GCloud Docker Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet

# 1 & 2. Build and Push (Parallel/Conditional)
echo -e "${BLUE}==> Step 1 & 2: Building and pushing (MODE: $MODE)...${NC}"

if [ "$MODE" == "all" ]; then
    # Parallel execution
    # Using subshells to capture output so it doesn't scramble with main process or other subshell
    ( deploy_fe ) &
    pid_fe=$!
    ( deploy_be ) &
    pid_be=$!
    
    wait $pid_fe || exit 1
    wait $pid_be || exit 1
elif [ "$MODE" == "fe" ]; then
    deploy_fe
elif [ "$MODE" == "be" ]; then
    deploy_be
else
    echo -e "${RED}Unknown mode: $MODE (Available: all, fe, be)${NC}"
    exit 1
fi

# 3. Upload config
echo -e "${BLUE}==> Step 3: Uploading configs...${NC}"
run_step "Preparing .env.vps" bash -c "cp .env .env.vps && echo 'FE_IMAGE=${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest' >> .env.vps && echo 'BE_IMAGE=${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest' >> .env.vps"
run_step "Config transmission to GCS" gcloud storage cp .env.vps docker-compose.yml Caddyfile gs://${BUCKET_NAME}/vps/ --project=${PROJECT_ID}

# 4. Deploy to VPS
echo -e "${BLUE}==> Step 4: VPS Command Execution...${NC}"
run_step "Remote Restart on VPS" gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command "
  mkdir -p ~/message-consolidator && cd ~/message-consolidator && 
  gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet &&
  gcloud storage cp gs://${BUCKET_NAME}/vps/.env.vps .env && 
  gcloud storage cp gs://${BUCKET_NAME}/vps/docker-compose.yml . && 
  gcloud storage cp gs://${BUCKET_NAME}/vps/Caddyfile . && 
  sudo docker-compose pull || sudo docker compose pull &&
  sudo docker-compose up -d --remove-orphans || sudo docker compose up -d --remove-orphans
"

# 5. Verification
echo -e "${BLUE}==> Step 5: Verifying deployment...${NC}"

# If only FE, we might skip BE startup log check or just check anyway. Let's check anyway to be safe.
echo -n "Waiting for Backend health... "
MAX_RETRIES=20
RETRY_COUNT=0
IS_READY=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  if gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command "sudo docker logs message-consolidator-backend 2>&1 | grep 'Startup Complete'" > /dev/null 2>&1; then
    IS_READY=1
    break
  fi
  echo -n "."
  sleep 2
  RETRY_COUNT=$((RETRY_COUNT+1))
done

if [ $IS_READY -eq 1 ]; then
  echo -e "\n${GREEN}✅ Backend Startup Complete log found!${NC}"
else
  echo -e "\n${RED}❌ Timeout: Backend Startup Complete log not found.${NC}"
fi

HEALTH_CHECK_URL="https://34.67.133.18.nip.io/health"
run_step "External Health Check (HTTPS)" bash -c "curl -s -k '$HEALTH_CHECK_URL' | grep -q 'OK'"

echo -e "\n${GREEN}🚀 Deployment Successful (MODE: $MODE)!${NC}"
