import { state } from './state.js';

/**
 * 전역 응답 처리 함수. 401 Unauthorized 발생 시 인증 에러를 던집니다.
 */
async function handleResponse(resp, customMsg) {
    if (resp.status === 401) {
        const err = new Error('Unauthorized');
        err.isAuthError = true;
        throw err;
    }

    const contentType = resp.headers.get("content-type");
    if (contentType && contentType.indexOf("text/html") !== -1) {
        // If we expected JSON but got HTML, it's likely a redirect to login
        const err = new Error('Session Expired or Unauthorized');
        err.isAuthError = true;
        throw err;
    }

    if (!resp.ok) {
        const text = await resp.text();
        throw new Error(customMsg || text || `Error ${resp.status}`);
    }

    if (contentType && contentType.indexOf("application/json") !== -1) {
        return await resp.json();
    }
    return resp;
}

export const api = {
    async fetchMessages(lang) {
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[lang] || 'Korean';
        const resp = await fetch(`/api/messages?lang=${langParam}`);
        return handleResponse(resp, 'Fetch messages failed');
    },

    async toggleDone(id, done) {
        const resp = await fetch('/api/messages/done', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, done })
        });
        return handleResponse(resp, 'Toggle done failed');
    },

    async deleteTask(idOrIds) {
        const body = Array.isArray(idOrIds) ? { ids: idOrIds } : { id: idOrIds };
        const resp = await fetch('/api/messages/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        return handleResponse(resp, 'Delete task failed');
    },

    async hardDeleteTasks(ids) {
        const resp = await fetch('/api/messages/hard-delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        return handleResponse(resp, 'Hard delete failed');
    },

    async restoreTasks(ids) {
        const resp = await fetch('/api/messages/restore', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids })
        });
        return handleResponse(resp, 'Restore failed');
    },

    async fetchWhatsAppStatus() {
        const resp = await fetch('/api/whatsapp/status');
        return handleResponse(resp, 'WA status check failed');
    },

    async fetchSlackStatus() {
        const resp = await fetch('/api/slack/status');
        return handleResponse(resp, 'Slack status check failed');
    },

    async triggerScan(lang) {
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[lang] || 'Korean';
        const resp = await fetch(`/api/scan?lang=${langParam}`);
        return handleResponse(resp, 'Scan failed');
    },

    async translateTasks(lang) {
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[lang] || 'Korean';
        const resp = await fetch(`/api/translate?lang=${langParam}`, { method: 'POST' });
        return handleResponse(resp, 'Translation failed');
    },

    async fetchArchive(params = {}) {
        const query = new URLSearchParams();
        const langMap = { 'ko': 'Korean', 'en': 'English', 'id': 'Indonesian', 'th': 'Thai' };
        const langParam = langMap[params.lang] || 'Korean';

        if (params.q) query.set('q', params.q);
        if (params.limit) query.set('limit', params.limit);
        if (params.offset) query.set('offset', params.offset);
        if (params.sort) query.set('sort', params.sort);
        if (params.order) query.set('order', params.order);
        query.set('lang', langParam);

        const resp = await fetch(`/api/messages/archive?${query.toString()}`);
        return handleResponse(resp, 'Fetch archive failed');
    },

    async fetchArchiveCount(q = '') {
        const query = q ? `?q=${encodeURIComponent(q)}` : '';
        const resp = await fetch(`/api/messages/archive/count${query}`);
        return handleResponse(resp, 'Fetch archive count failed');
    },

    async fetchUserProfile() {
        const resp = await fetch('/api/user/info');
        return handleResponse(resp, 'User info fetch failed');
    },

    async fetchAliases() {
        const resp = await fetch('/api/user/aliases');
        return handleResponse(resp, 'Fetch aliases failed');
    },

    async fetchUserStats() {
        // 사용자의 브라우저 타임존을 자동 추출 (예: 'Asia/Seoul', 'Europe/London')
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        const resp = await fetch('/api/user/stats', {
            headers: { 'X-Timezone': tz }
        });
        return handleResponse(resp, 'Fetch user stats failed');
    },

    async fetchAchievements() {
        const resp = await fetch('/api/achievements');
        return handleResponse(resp, 'Fetch achievements failed');
    },

    async fetchUserAchievements() {
        const resp = await fetch('/api/user/achievements');
        return handleResponse(resp, 'Fetch user achievements failed');
    },

    async addAlias(alias) {
        const resp = await fetch('/api/user/alias/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        return handleResponse(resp, 'Add alias failed');
    },

    async removeAlias(alias) {
        const resp = await fetch('/api/user/alias/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        return handleResponse(resp, 'Remove alias failed');
    },

    async getWhatsAppQR() {
        const resp = await fetch('/api/whatsapp/qr');
        return handleResponse(resp, 'QR fetch failed');
    },

    async logoutWhatsApp() {
        const resp = await fetch('/api/whatsapp/logout', { method: 'POST' });
        return handleResponse(resp, 'WhatsApp logout failed');
    },

    async fetchGmailStatus() {
        const resp = await fetch('/api/gmail/status');
        return handleResponse(resp, 'Gmail status check failed');
    },

    async disconnectGmail() {
        const resp = await fetch('/api/gmail/disconnect', { method: 'POST' });
        return handleResponse(resp, 'Gmail disconnect failed');
    },

    async buyStreakFreeze() {
        const resp = await fetch('/api/user/buy-freeze', { method: 'POST' });
        return handleResponse(resp, 'Purchase failed');
    },

    async fetchTenantAliases() {
        const resp = await fetch('/api/tenant/aliases');
        return handleResponse(resp, 'Fetch tenant aliases failed');
    },

    async addTenantAlias(original, primary) {
        const resp = await fetch('/api/tenant/alias/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ original, primary })
        });
        return handleResponse(resp, 'Add tenant alias failed');
    },

    async removeTenantAlias(original) {
        const resp = await fetch('/api/tenant/alias/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ original })
        });
        return handleResponse(resp, 'Remove tenant alias failed');
    },

    async fetchTokenUsage() {
        const resp = await fetch('/api/user/token-usage');
        return handleResponse(resp, 'Fetch token usage failed');
    },

    async fetchContactMappings() {
        const resp = await fetch('/api/contacts/mappings');
        return handleResponse(resp, 'Fetch contact mappings failed');
    },

    async addContactMapping(repName, aliases) {
        const resp = await fetch('/api/contacts/mapping/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ rep_name: repName, aliases })
        });
        return handleResponse(resp, 'Add contact mapping failed');
    },

    async removeContactMapping(repName) {
        const resp = await fetch('/api/contacts/mapping/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ rep_name: repName })
        });
        return handleResponse(resp, 'Remove contact mapping failed');
    },

    async fetchReleaseNotes() {
        const resp = await fetch('/api/release-notes');
        return handleResponse(resp, 'Fetch release notes failed');
    },

    async fetchOriginalMessage(id) {
        const resp = await fetch(`/api/messages/${id}/original`);
        return handleResponse(resp, 'Fetch original message failed');
    },

    /**
     * Aggregates status from all channels and normalizes them.
     */
    async getChannelStatus() {
        const [slack, whatsapp, gmail] = await Promise.all([
            this.fetchSlackStatus().catch(() => ({ status: 'DISCONNECTED' })),
            this.fetchWhatsAppStatus().catch(() => ({ status: 'DISCONNECTED' })),
            this.fetchGmailStatus().catch(() => ({ connected: false }))
        ]);

        return {
            slack: slack?.status === 'CONNECTED',
            whatsapp: whatsapp?.status === 'CONNECTED',
            gmail: gmail?.connected === true
        };
    }
};
