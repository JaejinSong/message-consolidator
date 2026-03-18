import { state } from './state.js';

export const api = {
    async fetchMessages() {
        const resp = await fetch('/api/messages');
        if (!resp.ok) throw new Error(`Fetch messages failed: ${resp.status}`);
        return await resp.json();
    },

    async toggleDone(id, done) {
        const resp = await fetch('/api/messages/done', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, done })
        });
        if (!resp.ok) throw new Error(`Toggle done failed: ${resp.status}`);
        return resp;
    },

    async deleteTask(idOrIds) {
        const body = Array.isArray(idOrIds) ? { ids: idOrIds } : { id: idOrIds };
        const resp = await fetch('/api/messages/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!resp.ok) throw new Error(`Delete task failed: ${resp.status}`);
        return resp;
    },

    async hardDeleteTasks(ids) {
        const resp = await fetch('/api/messages/hard-delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        if (!resp.ok) throw new Error(`Hard delete failed: ${resp.status}`);
        return resp;
    },

    async restoreTasks(ids) {
        const resp = await fetch('/api/messages/restore', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        if (!resp.ok) throw new Error(`Restore failed: ${resp.status}`);
        return resp;
    },

    async fetchWhatsAppStatus() {
        const resp = await fetch('/api/whatsapp/status');
        if (!resp.ok) throw new Error(`WA status check failed: ${resp.status}`);
        return await resp.json();
    },

    async triggerScan(lang) {
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[lang] || 'Korean';
        return await fetch(`/api/scan?lang=${langParam}`);
    },

    async translateTasks(lang) {
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[lang] || 'Korean';
        const resp = await fetch(`/api/translate?lang=${langParam}`, { method: 'POST' });
        if (!resp.ok) throw new Error(`Translation failed: ${resp.status}`);
        return await resp.json();
    },

    async fetchArchive() {
        const resp = await fetch('/api/messages/archive');
        if (!resp.ok) throw new Error(`Fetch archive failed: ${resp.status}`);
        return await resp.json();
    },

    async fetchUserProfile() {
        const resp = await fetch('/api/user/info');
        if (!resp.ok) throw new Error(`User info fetch failed: ${resp.status}`);
        return await resp.json();
    },

    async fetchAliases() {
        const resp = await fetch('/api/user/aliases');
        if (!resp.ok) throw new Error(`Fetch aliases failed: ${resp.status}`);
        return await resp.json();
    },

    async addAlias(alias) {
        const resp = await fetch('/api/user/alias/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        if (!resp.ok) throw new Error(`Add alias failed: ${resp.status}`);
        return resp;
    },

    async removeAlias(alias) {
        const resp = await fetch('/api/user/alias/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        if (!resp.ok) throw new Error(`Remove alias failed: ${resp.status}`);
        return resp;
    },

    async getWhatsAppQR() {
        const resp = await fetch('/api/whatsapp/qr');
        if (!resp.ok) throw new Error(`QR fetch failed: ${resp.status}`);
        return await resp.json();
    },

    async fetchGmailStatus() {
        const resp = await fetch('/api/gmail/status');
        if (!resp.ok) throw new Error(`Gmail status check failed: ${resp.status}`);
        return await resp.json();
    }
};
