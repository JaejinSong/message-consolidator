# FAQ & Privacy

## Frequently Asked Questions

**Can other users see my tasks?**
No. All data is isolated by email — no user can see another's tasks or messages.

**If I disconnect a channel, do my existing tasks disappear?**
No. Disconnecting only stops new message collection. Existing tasks remain until you delete them.

**A message isn't showing up in my dashboard. What do I do?**
Check in order:
1. Is the channel showing **Connected**? If not, reconnect.
2. Is the message work-related? AI filters out greetings and casual chat.
3. Was it caught by the noise filter? (Gmail marketing is auto-blocked.)
4. Click **🔄 Scan Now** to force an immediate scan.
5. If still missing, contact your admin.

**The AI extracted the wrong requester or assignee.**
Use the **Edit** button on the task to fix it manually. If you're often missing from the Received tab, add your names and aliases in **Settings → My Aliases**.

**Does Gemini learn from my messages?**
Your messages are sent to the Gemini API for task extraction. Whether Google uses API data for training is governed by Google's API data policy. If you have sensitive conversations, consider not connecting that channel.

**Do all my WhatsApp group messages come in?**
Yes — all 1-on-1 chats and groups you're a member of are monitored. Per-conversation blocking is not currently supported.

**Can I restore a deleted task?**
Soft-deleted tasks (Archive → Cancelled) can be restored. **Permanently deleted** tasks cannot be recovered.

**How do I get a report in a different language?**
Open the report and click **Translate**. The original is kept and a translated version is added.

**A person's name is displayed incorrectly.**
Go to **Settings → Display Name Override** and map the incorrect display name to the correct one.

**How does Slack get connected automatically?**
Slack uses a workspace-level Bot Token set up by your admin. When you sign in, your Google email is matched to your Slack account email automatically.

---

## Privacy & Data Summary

| Channel | Permission | What the app does |
|---|---|---|
| Gmail | Read-only (`gmail.readonly`) | Cannot send, delete, or label your mail |
| WhatsApp | WhatsApp Web equivalent | Receive messages only |
| Telegram | MTProto user API | Reads all conversations you're a member of |
| Slack | Admin-configured Bot Token | Reads channel messages and user info |

**Where is my data stored?**
All data is stored in this instance's own database (Turso/SQLite). Message content is sent to the Google Gemini API for task extraction — it is not forwarded to any other external service (unless you explicitly export to Notion).

**Session security:**
Login sessions use HttpOnly cookies (not accessible by JavaScript). Gmail OAuth tokens and Telegram sessions are stored in the instance database — this requires trusting your instance admin.

**How to remove your data:**
- **Disconnect a channel** → stops new collection; existing tasks remain.
- **Delete tasks** → soft delete goes to Archive; permanent delete removes from DB.
- **Delete your account** → not available in the UI; contact your admin.

To fully revoke access after disconnecting:
- Gmail: [Google Account → Security → Third-party apps](https://myaccount.google.com/permissions)
- Telegram: [my.telegram.org/apps](https://my.telegram.org/apps) → delete the app
