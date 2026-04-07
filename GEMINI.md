# [Project Specific: Project GEM Addon]

## 0. Mandatory Operation Protocol (Serena MCP)
- **Zero-Guessing Rule:** BEFORE generating/modifying code, you MUST use `serena` tools (`find_symbol`, `search_for_pattern`, `list_dir`) to map exact references.
- **Anti-Brute Force Policy (Strict):** You are STRICTLY FORBIDDEN from using native file reading tools (e.g., `read_file`, `analyze` with large line ranges) on source code files (e.g., `.go`, `.ts`). You MUST exclusively rely on Serena's AST-based extraction.
- **Exception (Config Files):** Native file reading tools are ALLOWED ONLY for configuration and plain text files (e.g., `.yaml`, `.json`, `.md`, `.env`) where symbol-based LSP analysis is impossible.
- **Failure Protocol:** If `serena` returns an error or no results on code files, HALT execution. Explicitly ask the user for clarification. DO NOT fallback to base training data or hallucination.

## 1. Architecture & Migration
- **Strict Logic Delegation:** Go (1.25.6) = 100% Business Logic, Calculations, Aggregations. TypeScript (Vanilla + Vite) = UI Rendering, DOM, Events ONLY.
- **Migration Policy:** NO mutations on existing `.js`. Rewrite strictly in `.ts` following the delegation rule.
- **Database:** SQLite (Turso).

## 2. Design System (CSS)
- **Constraint:** BEM is mandatory. `px` and `hex` are strictly FORBIDDEN. Use `rem` and `variables.css` tokens.
- **Validation Check:** You MUST run `node verify-css.cjs` and confirm success before marking a task as complete.

## 3. Infrastructure & Monitoring
- **Observability:** Account for 150MB memory overhead strictly reserved for WhaTap agents.
- **Type Safety:** Explicit integer conversion for ALL ID parameters is MANDATORY across all layers.