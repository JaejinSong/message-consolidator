#!/bin/bash
set -e
set -o pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# Timer
START_TIME=$(date +%s)

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

# Validation: Explicit Integer Conversion Check
validate_int() {
    local name="$1"
    local value="$2"
    if ! [[ "$value" =~ ^[0-9]+$ ]]; then
        echo -e "${RED}Error: $name must be an integer (got: '$value')${NC}"
        exit 1
    fi
}

# Helper function to run a step and show progress
# Why: Handles log isolation for parallel execution
run_step() {
    local name="$1"
    shift
    local start_time=$(date +%s)
    local tmp_log=$(mktemp)
    
    # Run in background if requested (via special flag or handled outside)
    # Here we assume it's called with output redirection to isolate logs
    if "$@" > "$tmp_log" 2>&1; then
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        echo -e "[${GREEN} PASS ${NC}] $name (${duration}s)"
        rm -f "$tmp_log"
    else
        echo -e "[${RED} FAIL ${NC}] $name"
        echo -e "--------------------------------------------------------------------------------"
        cat "$tmp_log"
        echo -e "--------------------------------------------------------------------------------"
        rm -f "$tmp_log"
        return 1
    fi
}

deploy_fe() {
    echo -e "${BLUE}==> Frontend: Optimizing and Building...${NC}"
    run_step "FE: Optimizing CSS" npm run optimize:css
    run_step "FE: CSS Integrity" node verify-css.cjs
    run_step "FE: Building image" env DOCKER_BUILDKIT=1 docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest -f docker/frontend/Dockerfile .
    run_step "FE: Pushing image" docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest
}

deploy_be() {
    echo -e "${BLUE}==> Backend: Building...${NC}"
    run_step "BE: Building image" env DOCKER_BUILDKIT=1 docker build -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest -f docker/backend/Dockerfile .
    run_step "BE: Pushing image" docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest
}

upload_configs() {
    echo -e "${BLUE}==> Config: Preparing and uploading...${NC}"
    run_step "Preparing .env.vps" bash -c "cp .env .env.vps && echo 'FE_IMAGE=${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_FE}:latest' >> .env.vps && echo 'BE_IMAGE=${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_BE}:latest' >> .env.vps"
    run_step "Config transmission to GCS" gcloud storage cp .env.vps docker-compose.yml Caddyfile gs://${BUCKET_NAME}/vps/ --project=${PROJECT_ID}
}

# 0. Pre-deployment verification
echo -e "${BLUE}==> Step 0: Running tests in parallel (MODE: $MODE)...${NC}"
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

pids=()
# Why: Parallelizing tests to save time.
if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then
    run_step "Go unit tests" go test ./... & pids+=($!)
    run_step "AI Regression tests" go test -tags regression ./ai/... ./tests/regression/... & pids+=($!)
fi

if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then
    run_step "NPM (Vitest) tests" npm test & pids+=($!)
fi

# Wait for all tests
for pid in "${pids[@]}"; do
    wait "$pid" || { echo -e "${RED}Test failed. Aborting deployment.${NC}"; exit 1; }
done

# Docker Auth
run_step "GCloud Docker Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet

# 1, 2 & 3. Build, Push and Upload
echo -e "${BLUE}==> Step 1, 2 & 3: Building, pushing and uploading (MODE: $MODE)...${NC}"

if [ "$MODE" == "all" ]; then
    # Run builds and config upload in parallel threads to maximize speed
    ( deploy_fe ) &
    pid_fe=$!

    ( deploy_be ) &
    pid_be=$!

    ( upload_configs ) &
    pid_cfg=$!
    
    wait $pid_fe || { echo -e "${RED}FE deployment failed. Aborting.${NC}"; exit 1; }
    wait $pid_be || { echo -e "${RED}BE deployment failed. Aborting.${NC}"; exit 1; }
    wait $pid_cfg || { echo -e "${RED}Config upload failed. Aborting.${NC}"; exit 1; }
elif [ "$MODE" == "fe" ]; then
    deploy_fe
    upload_configs
elif [ "$MODE" == "be" ]; then
    deploy_be
    upload_configs
else
    echo -e "${RED}Unknown mode: $MODE (Available: all, fe, be)${NC}"
    exit 1
fi

# 4. Deploy to VPS
echo -e "${BLUE}==> Step 4: VPS Command Execution...${NC}"
run_step "Remote Restart on VPS" gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command "
  mkdir -p ~/message-consolidator && cd ~/message-consolidator &&
  (gcloud storage cp gs://${BUCKET_NAME}/vps/* . && mv .env.vps .env) &&
  sudo docker-compose up -d --pull always --force-recreate --remove-orphans
"

# 5. Verification
echo -e "${BLUE}==> Step 5: Verifying deployment...${NC}"

MAX_RETRIES=${DEPLOY_MAX_RETRIES:-20}
validate_int "MAX_RETRIES" "$MAX_RETRIES"

echo -n "Waiting for Backend health (Remote Polling)... "
IS_READY=$(gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID} --command "
  for i in \$(seq 1 $MAX_RETRIES); do
    if sudo docker logs message-consolidator-backend 2>&1 | grep -q 'Startup Complete'; then
      echo 1
      exit 0
    fi
    sleep 2
  done
  echo 0
" | tr -d '\r\n')

if [ "$IS_READY" == "1" ]; then
  echo -e "\n${GREEN}✅ Backend Startup Complete log found!${NC}"
else
  echo -e "\n${RED}❌ Timeout: Backend Startup Complete log not found.${NC}"
fi

HEALTH_CHECK_URL="https://34.67.133.18.nip.io/health"
run_step "External Health Check (HTTPS)" bash -c "curl -s -k '$HEALTH_CHECK_URL' | grep -q 'OK'"

# Execution Timer Calculation
END_TIME=$(date +%s)
TOTAL_DURATION=$((END_TIME - START_TIME))

echo -e "\n${GREEN}🚀 Deployment Successful (MODE: $MODE) in ${TOTAL_DURATION}s!${NC}"
