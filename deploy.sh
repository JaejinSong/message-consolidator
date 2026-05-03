#!/bin/bash
set -e
set -o pipefail

# --- Configuration ---
PROJECT_ID="gemini-enterprise-487906"
REGION="us-central1"
REPO_NAME="message-consolidator-repo"
VPS_NAME="chat-analyzer-vps"
VPS_PATH="~/message-consolidator"
HEALTH_URL="https://34.67.133.18.nip.io/health"
EXPECTED_ACCOUNT="jjsong@whatap.io"
REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}"
BUILDER_TAG="${REGISTRY}/backend-builder:latest"

# --- UI helpers ---
RED='\033[0;31m'; GREEN='\033[0;32m'; BLUE='\033[0;34m'; NC='\033[0m'
say_blue()  { echo -e "${BLUE}$*${NC}"; }
say_green() { echo -e "${GREEN}$*${NC}"; }
say_red()   { echo -e "${RED}$*${NC}"; }
fatal()     { say_red "FATAL: $*"; exit 1; }

# --- gcloud sanity check ---
# Why: shell may be on a personal gcloud config (e.g. `pocready-personal`).
# Pin the company config locally so docker credential helper / Artifact Registry
# push always use jjsong@whatap.io without mutating the user's global active config.
export CLOUDSDK_ACTIVE_CONFIG_NAME="default"
ACTIVE_ACCOUNT=$(gcloud config get-value account 2>/dev/null || true)
[ "$ACTIVE_ACCOUNT" = "$EXPECTED_ACCOUNT" ] || fatal "gcloud account mismatch (got '${ACTIVE_ACCOUNT}', expected '${EXPECTED_ACCOUNT}'). Check 'gcloud config configurations describe default'."
[ "$(gcloud config get-value project 2>/dev/null)" = "$PROJECT_ID" ] || fatal "gcloud project mismatch under config 'default'. Run: gcloud config set project ${PROJECT_ID} --configuration=default"

# --- SSH ---
SSH_OPTS="-o ControlMaster=auto -o ControlPath=~/.ssh/control-%C -o ControlPersist=10m -q"
SSH_CMD="ssh ${SSH_OPTS} ${VPS_NAME}"
say_blue "==> Pre-establishing SSH Master Connection..."
${SSH_CMD} -M -f -N || true

# --- CLI args ---
MODE="all"; FORCE_BUILDER="false"
for arg in "$@"; do
    case $arg in
        fe|be|all) MODE=$arg ;;
        --builder) FORCE_BUILDER="true" ;;
    esac
done
BUILD_FE=false; BUILD_BE=false
[[ "$MODE" == "all" || "$MODE" == "fe" ]] && BUILD_FE=true
[[ "$MODE" == "all" || "$MODE" == "be" ]] && BUILD_BE=true

# --- Env + tag ---
START_TIME=$(date +%s)
BUILD_TAG=$(date +%Y%m%d%H%M%S)
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}
IMAGE_FE_TAG="${REGISTRY}/frontend:${BUILD_TAG}"
IMAGE_BE_TAG="${REGISTRY}/backend:${BUILD_TAG}"

# --- run_step ---
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

# --- Build / Push helpers ---
# Why: Two tags share the same blob; registry dedups so only manifests differ.
# Parallel publish saves one manifest round-trip. Direct `docker push` is ~3s faster
# than `buildx build --push` because buildx adds graph re-evaluation + per-layer
# HEAD round-trips. Verified empirically (zstd/gzip/level-1/level-3 all equally
# slow vs docker push); the cost is buildx orchestration, not compression algo.
push_dual_tag() {
    local name="$1" t1="$2" t2="$3"
    run_step "$name" bash -c "
        docker push ${t1} > /dev/null 2>&1 & p1=\$!
        docker push ${t2} > /dev/null 2>&1 & p2=\$!
        wait \$p1 && wait \$p2
    "
}

# Why: tar | ssh streams all files through a single SSH channel — replaces scp,
# which (on OpenSSH 9.0+) uses SFTP and pays per-file open/close/fsync overhead.
# On e2-micro this is the difference between ~3s and ~0.5-1s for 2-3 small files.
upload_via_tar() {
    tar c "$@" | ${SSH_CMD} "cd ${VPS_PATH} && tar x"
}

build_fe() {
    run_step "FE: CSS Optimize" npm run optimize:css
    # Why: --provenance=false --sbom=false drops buildx attestation manifests (~5-15MB
    # per image on registry push) — internal-use images don't need supply-chain metadata.
    run_step "FE: Build" docker buildx build --platform linux/amd64 -q \
        --provenance=false --sbom=false \
        -t "${IMAGE_FE_TAG}" -t "${REGISTRY}/frontend:latest" \
        -f docker/frontend/Dockerfile \
        --load .
}

build_be() {
    if [[ "$FORCE_BUILDER" == "true" ]] || ! docker image inspect "$BUILDER_TAG" >/dev/null 2>&1; then
        run_step "BE: Builder" docker build --platform linux/amd64 -q -t "$BUILDER_TAG" -f docker/backend/Dockerfile.builder .
        # Builder push is rare, can happen in background
        docker push "$BUILDER_TAG" > /dev/null 2>&1 &
    fi
    # Why: --load builds to local daemon without push, allowing the build to run in
    # parallel with Stage 1 tests. Push is gated on test success in Stage 2.
    # --provenance=false --sbom=false drops buildx attestation manifests (~5-15MB).
    run_step "BE: Build" docker buildx build --platform linux/amd64 -q \
        --provenance=false --sbom=false \
        -t "${IMAGE_BE_TAG}" -t "${REGISTRY}/backend:latest" \
        -f docker/backend/Dockerfile \
        --build-arg BUILDER_IMAGE="$BUILDER_TAG" \
        --load .
}

# --- Stage 1: Tests + Builds + Auth (parallel) ---
# Why: Builds use buildx --load (no push) so they can overlap with the test gate.
# Push happens in Stage 2 only after tests pass — failed tests don't pollute registry.
echo
say_blue "=================================================="
say_blue "==> STAGE 1: Tests + Builds (parallel)"
say_blue "=================================================="

p_test_go=""; p_test_ai=""; p_test_node=""; p_build_be=""; p_build_fe=""
if $BUILD_BE; then
    ( run_step "Go Unit Tests" go test ./... ) & p_test_go=$!
    ( run_step "AI Regressions" make test-ai ) & p_test_ai=$!
    ( build_be ) & p_build_be=$!
fi
if $BUILD_FE; then
    ( run_step "NPM (Vitest)" npm test ) & p_test_node=$!
    ( build_fe ) & p_build_fe=$!
fi
( run_step "GCloud Auth" gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet ) & p_auth=$!

# Test gate (fail fast — built images stay local, never pushed)
[ -n "$p_test_go" ]   && { wait $p_test_go   || fatal "Go Tests Failed"; }
[ -n "$p_test_ai" ]   && { wait $p_test_ai   || fatal "AI Regressions Failed"; }
[ -n "$p_test_node" ] && { wait $p_test_node || fatal "Node Tests Failed"; }
wait $p_auth || fatal "GCloud Auth Failed"

# Build gate (tests passed — now require builds to have succeeded too)
[ -n "$p_build_be" ] && { wait $p_build_be || fatal "BE Build Failed"; }
[ -n "$p_build_fe" ] && { wait $p_build_fe || fatal "FE Build Failed"; }

say_green "Stage 1 passed! Tests + builds validated."

# --- Stage 2: Push + Deploy (parallel chains) ---
echo
say_blue "=================================================="
say_blue "==> STAGE 2: Push + Deploy (parallel chains)"
say_blue "=================================================="

# 2.0 Sync orchestration files to VPS
say_blue "==> Syncing Orchestration Files..."
grep -vE '^(FE_IMAGE|BE_IMAGE)=' .env > .env.vps
if $BUILD_FE; then echo "FE_IMAGE=${IMAGE_FE_TAG}" >> .env.vps; else grep '^FE_IMAGE=' .env >> .env.vps || true; fi
if $BUILD_BE; then echo "BE_IMAGE=${IMAGE_BE_TAG}" >> .env.vps; else grep '^BE_IMAGE=' .env >> .env.vps || true; fi

# Why: Skip Caddyfile upload + reload when local content matches VPS copy.
# Empty remote hash (file missing / SSH error) defaults to CADDY_CHANGED=true (safe).
LOCAL_CADDY_HASH=$(sha256sum Caddyfile | awk '{print $1}')
REMOTE_CADDY_OUT=$(${SSH_CMD} "sha256sum ${VPS_PATH}/Caddyfile 2>/dev/null" 2>/dev/null || true)
REMOTE_CADDY_HASH="${REMOTE_CADDY_OUT%% *}"
CADDY_CHANGED=true
[ -n "$REMOTE_CADDY_HASH" ] && [ "$LOCAL_CADDY_HASH" = "$REMOTE_CADDY_HASH" ] && CADDY_CHANGED=false

UPLOAD_FILES=(.env.vps docker-compose.yml)
if $CADDY_CHANGED; then UPLOAD_FILES+=(Caddyfile); fi
run_step "Upload Configs" upload_via_tar "${UPLOAD_FILES[@]}"
${SSH_CMD} "cd ${VPS_PATH} && mv .env.vps .env"

# 2.1 Start chains (push + deploy inlined per service)
p_be=""; p_fe=""
if $BUILD_BE; then
    (
        push_dual_tag "BE: Push" "${IMAGE_BE_TAG}" "${REGISTRY}/backend:latest"
        say_blue "==> Deploying Backend Container..."
        # Why: Pre-remove handles orphan containers created outside compose context;
        # --force-recreate alone fails when the container wasn't tracked by this compose project.
        run_step "BE: Deploy" ${SSH_CMD} "cd ${VPS_PATH} && sudo docker rm -f message-consolidator-backend 2>/dev/null || true && sudo docker compose up -d --force-recreate backend"
        # Why: Poll readiness inline so chain_fe (still running) absorbs the wait, and
        # the time becomes a visible PASS line instead of invisible post-deploy delay.
        # Why: sleep 0.5 (vs 2) lifts polling round-up cost by 4x — same 60s budget,
        # 4x finer detection. Each iter ~0.5-0.8s incl. docker logs grep.
        run_step "BE: Startup" ${SSH_CMD} "
            for i in \$(seq 1 120); do
                sudo docker logs message-consolidator-backend 2>&1 | grep -qi 'startup complete' && exit 0
                sleep 0.5
            done
            echo 'Backend did not signal startup complete within 60s' >&2
            exit 1
        "
    ) & p_be=$!
fi
if $BUILD_FE; then
    (
        push_dual_tag "FE: Push" "${IMAGE_FE_TAG}" "${REGISTRY}/frontend:latest"
        say_blue "==> Deploying Frontend Container..."
        run_step "FE: Deploy" ${SSH_CMD} "cd ${VPS_PATH} && sudo docker rm -f message-consolidator-frontend 2>/dev/null || true && sudo docker compose up -d --force-recreate frontend"
    ) & p_fe=$!
fi
p_caddy=""
if $CADDY_CHANGED; then
    (
        say_blue "==> Deploying Caddy Configuration..."
        # Why: Reloading Caddy in-place for zero-downtime config updates.
        run_step "Caddy: Reload" ${SSH_CMD} "cd ${VPS_PATH} && sudo docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile" || \
        run_step "Caddy: Restart" ${SSH_CMD} "cd ${VPS_PATH} && sudo docker compose restart caddy"
    ) & p_caddy=$!
else
    say_blue "==> Caddyfile unchanged — skip reload"
fi

# 2.2 Wait for convergence
[ -n "$p_be" ]    && { wait $p_be    || exit 1; }
[ -n "$p_fe" ]    && { wait $p_fe    || exit 1; }
[ -n "$p_caddy" ] && { wait $p_caddy || exit 1; }

echo
say_green "Stage 2 complete! Infrastructure updated."

# --- Post-Deployment ---
echo
say_blue "==> Final Post-Deployment Verification..."
run_step "Health Check" bash -c "curl -s -k '${HEALTH_URL}' | grep -q 'OK'"

echo
say_green "🚀 Full Stack Deployed in $(( $(date +%s) - START_TIME ))s!"

# Why: Cleans up dangling images to prevent VPS disk space exhaustion.
run_step "Cleanup: Prune Images" ${SSH_CMD} "sudo docker image prune -f"
