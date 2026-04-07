# Message Consolidator PRD

## Overview
A smart dashboard to manage business messages from Slack, WhatsApp, and Gmail in one place.

## Core Features
1. **Task Management**:
   - Aggregate messages from multiple sources.
   - Categorize messages as TODO, Done, or Deleted.
   - Filter by source, category, and assignee.
   - Identity Resolution across sources (e.g., link a WhatsApp number to a Slack email).

2. **Reporting**:
   - Generate AI summaries of task progress.
   - Visualize data using Sankey/Topology charts for Requester-Worker relationships.
   - Group contacts by type: Internal, Partner, Customer, or None.

3. **Gamification**:
   - Track user points, XP, and levels.
   - Maintain daily streaks and allow streak freezes.

## User Flow
1. Login via Google OAuth.
2. View the main dashboard with the latest tasks.
3. Mark tasks as "Done" to archive them.
4. Go to the "Insights" tab to view performance metrics.
5. Search for and link contact identities in the settings.

## System Constraints
- Persistence: SQLite (Turso).
- Backend: Go with Gorilla Mux.
- Frontend: TypeScript (Vanilla + Vite).
- Styling: BEM CSS.
