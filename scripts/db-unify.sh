#!/bin/bash
set -e

# 1. Load Turso credentials from .env
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

echo "--- Step 1: Syncing Turso to local db/test.db ---"
go run cmd/mc-util/*.go db-sync

echo "--- Step 2: Creating .env.local for local overrides ---"
cat <<EOF > .env.local
# Local override: Use unified test DB
TURSO_DATABASE_URL=file:db/test.db
EOF

echo "--- Step 3: Cleaning up old database files ---"
OLD_DBS=(
    "data.db"
    "messages.db"
    "test_report.db"
    "db/consolidator.db"
    "db/message_consolidator.db"
    "store/message_consolidator.db"
)

for db in "${OLD_DBS[@]}"; do
    if [ -f "$db" ]; then
        echo "Deleting old DB: $db"
        rm -f "$db"
    else
        echo "Skipping $db (not found)"
    fi
done

echo "--- Success: Unified test DB initialized at db/test.db ---"
echo "You can now run 'npm run dev' or 'go run main.go' using local data."
