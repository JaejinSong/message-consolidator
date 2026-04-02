# [Project Specific: Project GEM Addon]

## 1. Architecture & Migration
- **Logic Delegation (Strict):** **Go strictly handles ALL calculations, aggregations, and business logic.** TypeScript is explicitly restricted to UI rendering, DOM manipulation, and event passing.
- **Migration Rule:** **DO NOT modify existing `.js`.** All new code must be rewritten in `.ts` strictly adhering to the Logic Delegation rule above.
- **Stack:** Go 1.25.6, SQLite (Turso), Vite + Vanilla TS, WhaTap.

## 2. Design System (CSS)
- **Standard:** BEM mandatory. NO `px` or `hex`. Use `rem` or `variables.css` tokens.
- **Validation:** Run `node verify-css.cjs` before completion.

## 3. Infrastructure & Monitoring
- **WhaTap:** Acknowledge 150MB memory overhead by agents.
- **Type Safety:** Mandatory explicit integer conversion for all ID parameters.