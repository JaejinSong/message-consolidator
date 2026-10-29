#!/usr/bin/env bash
# Why: CLAUDE.md rules "no hand edits to sqlc output" and "UI is TypeScript only" are mechanical, so a hook enforces them.
input=$(cat)
tool=$(jq -r '.tool_name // ""' <<<"$input")
path=$(jq -r '.tool_input.file_path // .tool_input.relative_path // ""' <<<"$input")
[ -z "$path" ] && exit 0

# Why: db/ also holds hand-written files (proposals.sql.go), so the sqlc header decides, not the path.
case "$path" in
  db/*.go|*/db/*.go)
    if [ -f "$path" ] && head -1 "$path" | grep -q "Code generated"; then
      echo "BLOCKED: $path is sqlc output. Edit store/queries/*.sql and run 'sqlc generate'." >&2
      exit 2
    fi ;;
esac

case "$path" in
  src/*.js|src/*.jsx|*/src/*.js|*/src/*.jsx|static/*.js|static/*.jsx|*/static/*.js|*/static/*.jsx)
    if [ "$tool" = "Write" ] && [ ! -e "$path" ]; then
      echo "BLOCKED: new .js/.jsx is not allowed; write TypeScript (.ts/.tsx)." >&2
      exit 2
    fi ;;
esac
exit 0
