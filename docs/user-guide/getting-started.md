# Getting Started

## What is Message Consolidator?

Message Consolidator pulls work requests from Slack, WhatsApp, Gmail, and Telegram into a single dashboard. AI automatically extracts tasks, classifies them, and keeps them up to date.

Each message is classified as one of three states:

| State | Meaning |
|---|---|
| **NEW** | A new task has arrived — shown as a new item in the dashboard |
| **UPDATE** | A reply or progress update for an existing task |
| **RESOLVE** | The task is complete — moved to the archive automatically |

> [!NOTE]
> Your data is private. Every user's tasks and messages are isolated by email — you never see another user's work.

---

## First Login

1. Go to the instance URL provided by your admin.
2. Click **Sign in with Google**.
3. Select your Google account and grant the requested permissions.
4. You'll be taken to your dashboard automatically.

No sign-up form needed — your account is created on first login.

---

## Dashboard Layout

```
┌─────────────────────────────────────────────────────┐
│  Header: [Slack][WhatsApp][Gmail][Telegram] status  │
│          [🔄 Scan Now]  [Noise filter card]          │
├─────────────────────────────────────────────────────┤
│  Tabs: [Received] [Delegated] [Referenced] [All]    │
├─────────────────────────────────────────────────────┤
│  Task list: Channel | Room | Task | Requester | …   │
└─────────────────────────────────────────────────────┘
```

- **Channel icons** in the header show connection status. Click any icon to open Settings → Connections.
- **Noise filter card** shows how many Gmail marketing / newsletter emails were blocked today and this month.
- **🔄 Scan Now** fetches new messages immediately (otherwise scanning runs automatically in the background).

---

## Recommended Setup Order

Connect your channels in this order for the smoothest experience:

1. **Gmail** — instant, most stable
2. **WhatsApp** — one QR scan and done
3. **Telegram** — requires a one-time App ID/Hash from my.telegram.org (~10 min)
4. **Slack** — no action needed; auto-mapped by your admin
