#!/bin/bash
set -e
set -o pipefail

# Colors & constants
RED='\033[0;31m'; GREEN='\033[0;32m'; BLUE='\033[0;34m'; NC='\033[0m'
START_TIME=$(date +%s)

# Configuration
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
VPS_NAME="chat-analyzer-vps"
REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}"

# SSH Configuration
SSH_OPTS="-o ControlMaster=auto -o ControlPath=~/.ssh/control-%C -o ControlPersist=10m -q"
SSH_CMD="ssh ${SSH_OPTS} ${VPS_NAME}"
SCP_CMD="scp ${SSH_OPTS}"

# Establish background master connection
echo -e "${BLUE}==> Pre-establishing SSH Master Connection...${NC}"
${SSH_CMD} -M -f -N || true

# CLI Arguments
MODE="all"; FORCE_BUILDER="false"
for arg in "$@"; do
    case $arg in
        fe|be|all) MODE=$arg ;;
        --builder) FORCE_BUILDER="true" ;;
    esac
done

# Build Tags
BUILD_TAG=$(date +%Y%m%d%H%M%S)

# Load Environment
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

# Final image vars
IMAGE_FE_TAG="${REGISTRY}/frontend:${BUILD_TAG}"
IMAGE_BE_TAG="${REGISTRY}/backend:${BUILD_TAG}"
FINAL_FE_IMAGE=${FE_IMAGE:-"${REGISTRY}/frontend:latest"}
FINAL_BE_IMAGE=${BE_IMAGE:-"${REGISTRY}/backend:latest"}

# --- Helpers ---

run_step() {
    local name="$1"; shift
    local s_time=$(date +%s); local tmp_log=$(mktemp)
    if "$@" > "$tmp_log" 2>&1; then
        echo -e "[${GREEN} PASS ${NC}] $name ($(( $(date +%s) - s_time ))s)"
        rm -f "$tmp_log"
    else
        echo -e "[${RED} FAIL ${NC}] $name\n$(cat "$tmp_log")"
        rm -f "$tmp_log"; exit 1
    fi
}

# --- Execution ---

# Step 0: Testing (Parallel)
echo -e "${BLUE}==> Step 0: Parallel Testing...${NC}"
(
    run_step "Go Unit Tests" go test ./...
) & p1=$!
(
    run_step "AI Regressions" go test -tags regression ./ai/...
) & p2=$!
(
    run_step "NPM (Vitest)" npm test
) & p3=$!

wait $p1 || exit 1
wait $p2 || exit 1
wait $p3 || exit 1

run_step "GCloud Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet

# Step 1-3: Parallel Build & Push
echo -e "${BLUE}==> Step 1-3: Parallel Build & Push...${NC}"

# Frontend Task
build_fe() {
    run_step "FE: Build" docker build --platform linux/amd64 -q -t "${IMAGE_FE_TAG}" -t "${REGISTRY}/frontend:latest" -f docker/frontend/Dockerfile .
    run_step "FE: Push" bash -c "docker push ${IMAGE_FE_TAG} > /dev/null 2>&1 && docker push ${REGISTRY}/frontend:latest > /dev/null 2>&1"
}

# Backend Task
build_be() {
    BUILDER_TAG="${REGISTRY}/backend-builder:latest"
    if [[ "$FORCE_BUILDER" == "true" ]] || ! docker image inspect "$BUILDER_TAG" >/dev/null 2>&1; then
        run_step "BE: Builder" docker build --platform linux/amd64 -q -t "$BUILDER_TAG" -f docker/backend/Dockerfile.builder .
        docker push "$BUILDER_TAG" > /dev/null 2>&1
    fi
    run_step "BE: Build" docker build --platform linux/amd64 -q -t "${IMAGE_BE_TAG}" -t "${REGISTRY}/backend:latest" -f docker/backend/Dockerfile --build-arg BUILDER_IMAGE="$BUILDER_TAG" .
    run_step "BE: Push" bash -c "docker push ${IMAGE_BE_TAG} > /dev/null 2>&1 && docker push ${REGISTRY}/backend:latest > /dev/null 2>&1"
}

if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then 
    run_step "FE: CSS Optimize" npm run optimize:css
    build_fe & fe_pid=$! 
fi
if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then 
    build_be & be_pid=$! 
fi

# Wait for builds and capture final image paths
[ -n "$fe_pid" ] && { wait $fe_pid || exit 1; FINAL_FE_IMAGE="${IMAGE_FE_TAG}"; }
[ -n "$be_pid" ] && { wait $be_pid || exit 1; FINAL_BE_IMAGE="${IMAGE_BE_TAG}"; }

# Step 4: VPS Orchestration
echo -e "${BLUE}==> Step 4: VPS Orchestration...${NC}"
run_step "Config & Restart" bash -c "
  grep -vE '^(FE_IMAGE|BE_IMAGE)=' .env > .env.vps && 
  echo 'FE_IMAGE=${FINAL_FE_IMAGE}' >> .env.vps && 
  echo 'BE_IMAGE=${FINAL_BE_IMAGE}' >> .env.vps &&
  ${SCP_CMD} .env.vps docker-compose.yml Caddyfile ${VPS_NAME}:~/message-consolidator/ &&
  ${SSH_CMD} \"cd ~/message-consolidator && mv .env.vps .env && sudo docker-compose --env-file .env down --remove-orphans > /dev/null 2>&1 && sudo docker-compose --env-file .env up -d --force-recreate > /dev/null 2>&1\"
"

# Step 5: Post-Deployment Verification
echo -e "${BLUE}==> Step 5: Post-Deployment Verification...${NC}"
echo -n "Waiting for Backend Startup... "
${SSH_CMD} -- "
  for i in \$(seq 1 30); do
    sudo docker logs message-consolidator-backend 2>&1 | grep -q 'Startup Complete' && exit 0
    sleep 2
  done
  exit 1
" && echo -e "${GREEN}Ready!${NC}" || { echo -e "${RED}Timeout!${NC}"; exit 1; }

run_step "Health Check" bash -c "curl -s -k 'https://34.67.133.18.nip.io/health' | grep -q 'OK'"

echo -e "\n${GREEN}🚀 Full Stack Deployed in $(( $(date +%s) - START_TIME ))s!${NC}"
