# [PROJECT: MESSAGE CONSOLIDATOR SETTINGS]

project_context: "Project GEM (Message Consolidator)"

mandatory_mcp_protocols:
  execute_sequential_thinking: 
    - "MANDATORY TRIGGER: You MUST execute the `@mcp:sequential-thinking` tool as the absolute first step."
    - "[CRITICAL] Output the resulting logic strictly inside a <thinking>...</thinking> block."
    - "Inside <thinking>: Analyze logic, check architecture sync, define edge cases."
    - "Thoughts MUST be in English ONLY for precision."
  execute_serena: 
    - "MANDATORY TRIGGER: You MUST execute the `@mcp:serena` tool to map exact symbols BEFORE writing any code."
    - "ANTI-BRUTE-FORCE: 100% reliance on `@mcp:serena` for ingestion. Native read_file or workspace search is FORBIDDEN."
  
tech_stack:
  backend: "Go 1.25.6, gorilla/mux, SQLite/Turso, whatsmeow, slack-go, sqlc"
  frontend: "Vite, Vanilla TS, Clean Architecture"
  infra: "Docker, Caddy, GCP
  pipeline: "Scanner -> AI Extraction -> DB -> Dashboard"

coding_constraints:
  go:
    - "Max 40 lines per function."
    - "Max 2-depth nesting (Strict Early Return / Guard Clauses)."
    - "Split files > 800 lines by Domain."
    - "Explicit Integer Conversion for all ID parameters."
  css_ui:
    - "CRITICAL: NO hardcoded px or hex values."
    - "ONLY use 'rem' (16px=1rem) or variables.css tokens."
    - "BEM methodology (c-block__element--modifier) is MANDATORY."
    - "Must pass 'node verify-css.cjs' before deployment."

development_process:
  validation: 
    - "Script-First: Verify logic via Node.js/Go scripts BEFORE UI testing."
    - "Bug-Fix-Test: All bug fixes require an independent test case."