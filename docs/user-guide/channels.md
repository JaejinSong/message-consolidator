# Connecting Channels

## How to Open the Connections Screen

All channel setup is in **Settings → Connections**:

- Click the ⚙️ gear icon (top right) → select the **Connections** tab, or
- Click any channel icon in the dashboard header.

Each channel card shows its current status and identification info. Status meanings:

| Status | Meaning |
|---|---|
| **Connected** | Working normally |
| **Not connected** | Needs setup |
| **Disconnected** | Token expired or device logged out — reconnect required |

---

## Gmail

**What it does:** Scans your inbox for work-related emails. Marketing and newsletters are automatically filtered out.

**You need:** The Google account you want to monitor (can differ from your login account).

**Steps:**
1. Open **Settings → Connections**.
2. Click **Connect** on the Gmail card.
3. Complete the Google OAuth consent screen.
4. The permission requested is **read-only** (`gmail.readonly`) — the app cannot send, delete, or modify your mail.
5. The card shows **Connected** when done.

To disconnect: Gmail card → **Disconnect**.

---

## WhatsApp

**What it does:** Monitors your 1-on-1 chats and group messages (same as WhatsApp Web).

**You need:** Your phone with WhatsApp installed.

**Steps:**
1. Open **Settings → Connections**.
2. Click **Connect** on the WhatsApp card — a QR modal appears.
3. On your phone, open WhatsApp → **Settings → Linked Devices → Link a Device**.
4. Scan the QR code shown on screen.
5. The card shows **Connected** with your device name when done.

> [!NOTE]
> The QR code expires in ~1 minute. If it expires, click **Re-scan QR** for a new one.

**Monitored:** 1-on-1 chats ✅, Group chats ✅, Broadcast channels ❌

---

## Telegram

**What it does:** Monitors your Telegram account (1-on-1, groups, and channels you're a member of).

**You need:** An App ID and App Hash from [my.telegram.org/apps](https://my.telegram.org/apps) (one-time setup, ~10 minutes).

### Step 1 — Get your App ID and App Hash (one time only)

1. Go to [https://my.telegram.org/apps](https://my.telegram.org/apps) and log in with your Telegram phone number.
2. Fill in any App title and short name, set Platform to **Web**.
3. Click **Create application**.
4. Save your **App ID** (a number) and **App Hash** (32-character string).

> [!IMPORTANT]
> Keep these credentials private — they are tied to your Telegram account.

### Step 2 — Connect in the app

1. Open **Settings → Connections**, click **Connect** on the Telegram card.
2. Enter your **App ID** and **App Hash** → click **Save and continue**.
3. Enter your phone number (with country code, e.g. `+821012345678`) → click **Send code**.
4. Enter the verification code from your Telegram app (or SMS).
5. If you have 2-step verification enabled, enter your cloud password as well.
6. The card shows **Connected** when done.

---

## Slack

**What it does:** Monitors workspace channels and DMs you're a member of.

**You need:** Nothing — Slack is set up by your admin using a workspace Bot Token. You are automatically mapped when you sign in with the same email as your Slack account.

If the Slack card shows "Auto-mapping not yet complete", your Slack email may differ from your Google login email. Contact your admin or add your Slack identifier in **Settings → Name Management Rules**.
