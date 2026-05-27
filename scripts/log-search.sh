#!/bin/bash
# Unified log search for Claude Code and manual debugging.
# Searches hot logs (VM host) and cold archive (GCS) for a given pattern.
#
# Usage:
#   ./scripts/log-search.sh <source> <date-range> <pattern>
#
#   source:     ai_inference | wa_messages | app (matches log directory)
#   date-range: YYYY-MM-DD or YYYY-MM-DD:YYYY-MM-DD
#   pattern:    grep-compatible string
#
# Examples:
#   ./scripts/log-search.sh ai_inference 2026-05-25 "classify"
#   ./scripts/log-search.sh app 2026-05-01:2026-05-25 "ERROR"
set -euo pipefail

SOURCE="${1:-}"
DATE_RANGE="${2:-}"
PATTERN="${3:-}"
LOG_DIR="${LOG_DIR:-$HOME/message-consolidator/logs}"
GCS_BUCKET="${GCS_BUCKET:-}"

if [ -z "$SOURCE" ] || [ -z "$DATE_RANGE" ] || [ -z "$PATTERN" ]; then
    echo "Usage: $0 <source> <date|date:date> <pattern>" >&2
    exit 1
fi

# Build date list from range
start_date="${DATE_RANGE%%:*}"
end_date="${DATE_RANGE##*:}"

dates=()
cur="$start_date"
while [[ "$cur" <= "$end_date" ]]; do
    dates+=("$cur")
    cur=$(date -d "$cur + 1 day" +%Y-%m-%d 2>/dev/null || date -v+1d -j -f "%Y-%m-%d" "$cur" +%Y-%m-%d)
done

found=0
for d in "${dates[@]}"; do
    # Hot path: VM host bind-mounted logs
    hot="${LOG_DIR}/${SOURCE}/${d}.jsonl"
    if [ -f "$hot" ]; then
        results=$(grep -n "$PATTERN" "$hot" 2>/dev/null || true)
        if [ -n "$results" ]; then
            echo "=== HOT: $hot ==="
            echo "$results"
            found=1
        fi
    fi

    # Also check lumberjack-style flat files (e.g. ai_inference.log)
    flat="${LOG_DIR}/${SOURCE}.log"
    if [ -f "$flat" ] && [ "$d" = "$start_date" ]; then
        results=$(grep -n "$PATTERN" "$flat" 2>/dev/null || true)
        if [ -n "$results" ]; then
            echo "=== HOT (flat): $flat ==="
            echo "$results"
            found=1
        fi
    fi

    # Cold path: GCS archive
    if [ -n "$GCS_BUCKET" ]; then
        gcs_path="gs://${GCS_BUCKET}/message-consolidator/${SOURCE}/${d}.jsonl.gz"
        if gcloud storage ls "$gcs_path" &>/dev/null; then
            results=$(gcloud storage cat "$gcs_path" | gunzip | grep -n "$PATTERN" 2>/dev/null || true)
            if [ -n "$results" ]; then
                echo "=== COLD: $gcs_path ==="
                echo "$results"
                found=1
            fi
        fi
    fi
done

if [ "$found" -eq 0 ]; then
    echo "[log-search] no matches for pattern '$PATTERN' in $SOURCE [$DATE_RANGE]"
fi
