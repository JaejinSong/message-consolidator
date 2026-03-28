#!/bin/bash
set -o pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# Load env and export for subshells
[ -f .env ] && { set -a; source .env; set +a; }
export GEMINI_API_KEY_FOR_TEST=${GEMINI_API_KEY_FOR_TEST:-$GEMINI_API_KEY}

echo -e "${BLUE}==> Starting Simultaneous Test Execution...${NC}"

# Temporary log files
LOG_GO=$(mktemp)
LOG_NPM=$(mktemp)
LOG_AI=$(mktemp)
LOG_UI=$(mktemp)

# Start times
START_TIME=$(date +%s)

# Run tasks in background
echo -ne "[ RUN ] Go Unit Tests... \r"
go test ./... > "$LOG_GO" 2>&1 &
PID_GO=$!

echo -ne "[ RUN ] NPM (Vitest) Tests... \r"
npm test > "$LOG_NPM" 2>&1 &
PID_NPM=$!

echo -ne "[ RUN ] AI Regression Tests... \r"
go test ./tests/regression > "$LOG_AI" 2>&1 &
PID_AI=$!

echo -ne "[ RUN ] Loading UI Verification... \r"
node tests/verify-loading-ui.cjs > "$LOG_UI" 2>&1 &
PID_UI=$!

# Wait for all and collect status
wait $PID_GO; STATUS_GO=$?
wait $PID_NPM; STATUS_NPM=$?
wait $PID_AI; STATUS_AI=$?
wait $PID_UI; STATUS_UI=$?

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

# Helper to print status
print_status() {
    local status=$1
    local name=$2
    if [ $status -eq 0 ]; then
        echo -e "[${GREEN} PASS ${NC}] $name"
    else
        echo -e "[${RED} FAIL ${NC}] $name"
    fi
}

echo -e "\n${BLUE}Final Test Summary (${DURATION}s):${NC}"
print_status $STATUS_GO "Go Unit Tests"
print_status $STATUS_NPM "NPM (Vitest) Tests"
print_status $STATUS_AI "AI Regression Tests"
print_status $STATUS_UI "Loading UI Verification"

# If any failed, show logs
GLOBAL_EXIT=0
if [ $STATUS_GO -ne 0 ] || [ $STATUS_NPM -ne 0 ] || [ $STATUS_AI -ne 0 ] || [ $STATUS_UI -ne 0 ]; then
    GLOBAL_EXIT=1
    echo -e "\n${RED}------------------- FAILURE LOGS -------------------${NC}"
    if [ $STATUS_GO -ne 0 ]; then
        echo -e "${RED}Failure in: Go Unit Tests${NC}"
        cat "$LOG_GO"
    fi
    if [ $STATUS_NPM -ne 0 ]; then
        echo -e "\n${RED}Failure in: NPM (Vitest) Tests${NC}"
        cat "$LOG_NPM"
    fi
    if [ $STATUS_AI -ne 0 ]; then
        echo -e "\n${RED}Failure in: AI Regression Tests${NC}"
        cat "$LOG_AI"
    fi
    if [ $STATUS_UI -ne 0 ]; then
        echo -e "\n${RED}Failure in: Loading UI Verification${NC}"
        cat "$LOG_UI"
    fi
    echo -e "----------------------------------------------------"
fi

# Cleanup
rm -f "$LOG_GO" "$LOG_NPM" "$LOG_AI" "$LOG_UI"

exit $GLOBAL_EXIT
