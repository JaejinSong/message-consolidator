# Managing Tasks

## Dashboard Tabs

| Tab | Shows |
|---|---|
| **Received** | Tasks assigned to you (you are the assignee) |
| **Delegated** | Tasks you asked others to do (you are the requester) |
| **Referenced** | Tasks you're indirectly involved in |
| **All** | Everything in one view |

Each task row shows: channel, room/thread, task description, requester, assignee, time, and status badges.

> [!TIP]
> **Stalled / Neglected** badges appear on tasks with no updates for 3+ business days — a signal to follow up.

---

## Task Actions

Click the action buttons on any task row:

| Action | Result |
|---|---|
| **✓ Complete** | Marks done → moved to Archive (Completed) |
| **Delete** (soft) | Moved to Archive (Cancelled) — can be restored |
| **Delete permanently** | Removed from DB — **cannot be undone** |
| **Edit** | Fix the title, requester, or assignee manually |
| **Merge** | Combine two duplicate tasks into one |
| **View original** | See the raw source message in a popup |
| **Open in [channel]** | Jump directly to the message in Slack, Telegram, etc. |

> [!WARNING]
> **AI errors are normal.** If the requester or assignee was extracted incorrectly, use **Edit** to fix it.

---

## Archive

Go to **Archive** in the top navigation.

| Tab | Content |
|---|---|
| **All** | Everything that has been archived |
| **Completed** | Tasks you marked as done |
| **Cancelled** | Soft-deleted tasks |
| **Merged** | Tasks combined via merge |

- Archived items can be **Restored** or **Deleted permanently**.
- Use the **Export** button to download as Excel, CSV, or JSON.

---

## Identity & Aliases

For tasks to appear in your **Received** tab, the AI needs to know what names you go by in messages.

**Add your aliases:** Settings → **My Aliases** — add any name or nickname others use to address you (e.g. `JJ`, `jjsong`, `재진`).

**Normalize display names:** Settings → **Name Management Rules** — map platform-specific names to a canonical name (e.g. WhatsApp `YOSEP PARK` → `박요셉`).

**Override a display name:** Settings → **Display Name Override** — force a specific name to appear in the UI (e.g. `재진` → `Jaejin Song`).
