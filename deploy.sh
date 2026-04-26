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

# --- Functions ---

# Frontend Build (load to local daemon; push happens after test gate)
build_fe() {
    run_step "FE: CSS Optimize" npm run optimize:css
    run_step "FE: Build" docker buildx build --platform linux/amd64 -q \
        -t "${IMAGE_FE_TAG}" -t "${REGISTRY}/frontend:latest" \
        -f docker/frontend/Dockerfile \
        --load .
}

push_fe() {
    # Why: Two tags share the same blob; registry dedups so only manifests differ.
    # Parallel publish saves one manifest round-trip.
    run_step "FE: Push" bash -c "
        docker push ${IMAGE_FE_TAG} > /dev/null 2>&1 & p1=\$!
        docker push ${REGISTRY}/frontend:latest > /dev/null 2>&1 & p2=\$!
        wait \$p1 && wait \$p2
    "
}

# Backend Build (load to local daemon; push happens after test gate)
build_be() {
    BUILDER_TAG="${REGISTRY}/backend-builder:latest"
    if [[ "$FORCE_BUILDER" == "true" ]] || ! docker image inspect "$BUILDER_TAG" >/dev/null 2>&1; then
        run_step "BE: Builder" docker build --platform linux/amd64 -q -t "$BUILDER_TAG" -f docker/backend/Dockerfile.builder .
        # Builder push is rare, can happen in background
        docker push "$BUILDER_TAG" > /dev/null 2>&1 &
    fi
    # Why: --load builds to local daemon without push, allowing the build to run in
    # parallel with Stage 1 tests. Push is gated on test success in Stage 2.
    run_step "BE: Build" docker buildx build --platform linux/amd64 -q \
        -t "${IMAGE_BE_TAG}" -t "${REGISTRY}/backend:latest" \
        -f docker/backend/Dockerfile \
        --build-arg BUILDER_IMAGE="$BUILDER_TAG" \
        --load .
}

push_be() {
    run_step "BE: Push" bash -c "
        docker push ${IMAGE_BE_TAG} > /dev/null 2>&1 & p1=\$!
        docker push ${REGISTRY}/backend:latest > /dev/null 2>&1 & p2=\$!
        wait \$p1 && wait \$p2
    "
}

# --- Execution ---

# --- Deployment Chains ---

chain_be() {
    push_be
    echo -e "${BLUE}==> Deploying Backend Container...${NC}"
    run_step "BE: Deploy" ${SSH_CMD} "cd ~/message-consolidator && sudo docker compose up -d --force-recreate backend"
}

chain_fe() {
    push_fe
    echo -e "${BLUE}==> Deploying Frontend Container...${NC}"
    run_step "FE: Deploy" ${SSH_CMD} "cd ~/message-consolidator && sudo docker compose up -d --force-recreate frontend"
}

chain_caddy() {
    echo -e "${BLUE}==> Deploying Caddy Configuration...${NC}"
    # Why: Reloading Caddy in-place for zero-downtime config updates.
    run_step "Caddy: Reload" ${SSH_CMD} "cd ~/message-consolidator && sudo docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile" || \
    run_step "Caddy: Restart" ${SSH_CMD} "cd ~/message-consolidator && sudo docker compose restart caddy"
}


# --- Execution Flow ---

# [STAGE 1] Parallel: Tests + Builds + Auth
# Why: Builds use buildx --load (no push) so they can overlap with the test gate.
# Push happens in Stage 2 only after tests pass — failed tests don't pollute registry.
echo -e "\n${BLUE}==================================================${NC}"
echo -e "${BLUE}==> STAGE 1: Tests + Builds (parallel)${NC}"
echo -e "${BLUE}==================================================${NC}"

p_test_go=""; p_test_ai=""; p_test_node=""; p_build_be=""; p_build_fe=""

if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then
    ( run_step "Go Unit Tests" go test ./... ) & p_test_go=$!
    ( run_step "AI Regressions" make test-ai ) & p_test_ai=$!
    ( build_be ) & p_build_be=$!
fi
if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then
    ( run_step "NPM (Vitest)" npm test ) & p_test_node=$!
    ( build_fe ) & p_build_fe=$!
fi
( run_step "GCloud Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet ) & p_auth=$!

# Test gate (fail fast — built images stay local, never pushed)
[[ -n "$p_test_go" ]] && { wait $p_test_go || { echo -e "${RED}FATAL: Go Tests Failed${NC}"; exit 1; }; }
[[ -n "$p_test_ai" ]] && { wait $p_test_ai || { echo -e "${RED}FATAL: AI Regressions Failed${NC}"; exit 1; }; }
[[ -n "$p_test_node" ]] && { wait $p_test_node || { echo -e "${RED}FATAL: Node Tests Failed${NC}"; exit 1; }; }
wait $p_auth || { echo -e "${RED}FATAL: GCloud Auth Failed${NC}"; exit 1; }

# Build gate (tests passed — now require builds to have succeeded too)
[[ -n "$p_build_be" ]] && { wait $p_build_be || { echo -e "${RED}FATAL: BE Build Failed${NC}"; exit 1; }; }
[[ -n "$p_build_fe" ]] && { wait $p_build_fe || { echo -e "${RED}FATAL: FE Build Failed${NC}"; exit 1; }; }

echo -e "${GREEN}Stage 1 passed! Tests + builds validated.${NC}"

# [STAGE 2] Parallel Push + Deploy Chains
echo -e "\n${BLUE}==================================================${NC}"
echo -e "${BLUE}==> STAGE 2: Push + Deploy (parallel chains)${NC}"
echo -e "${BLUE}==================================================${NC}"

# 2.0 Prep: Sync Config Files to VPS
echo -e "${BLUE}==> Syncing Orchestration Files...${NC}"
grep -vE '^(FE_IMAGE|BE_IMAGE)=' .env > .env.vps
if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then
    echo "FE_IMAGE=${IMAGE_FE_TAG}" >> .env.vps
else
    grep '^FE_IMAGE=' .env >> .env.vps || true
fi
if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then
    echo "BE_IMAGE=${IMAGE_BE_TAG}" >> .env.vps
else
    grep '^BE_IMAGE=' .env >> .env.vps || true
fi
run_step "Upload Configs" ${SCP_CMD} .env.vps docker-compose.yml Caddyfile ${VPS_NAME}:~/message-consolidator/
${SSH_CMD} "cd ~/message-consolidator && mv .env.vps .env"

# 2.1 Start Chains
p_be=""; p_fe=""; p_caddy=""

if [[ "$MODE" == "all" || "$MODE" == "be" ]]; then chain_be & p_be=$!; fi
if [[ "$MODE" == "all" || "$MODE" == "fe" ]]; then chain_fe & p_fe=$!; fi
chain_caddy & p_caddy=$!

# 2.2 Wait for Convergence
[ -n "$p_be" ] && { wait $p_be || exit 1; }
[ -n "$p_fe" ] && { wait $p_fe || exit 1; }
wait $p_caddy || exit 1

echo -e "\n${GREEN}Stage 2 complete! Infrastructure updated.${NC}"

# --- Post-Deployment ---

echo -e "\n${BLUE}==> Final Post-Deployment Verification...${NC}"
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

# Why: Cleans up dangling images to prevent VPS disk space exhaustion.
run_step "Cleanup: Prune Images" ${SSH_CMD} "sudo docker image prune -f"
