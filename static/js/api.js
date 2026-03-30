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
        const resp = await fetch(`/api/messages?lang=${lang}`);
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
        const resp = await fetch(`/api/scan?lang=${lang}`);
        return handleResponse(resp, 'Scan failed');
    },

    async translateTasks(lang) {
        const resp = await fetch(`/api/translate?lang=${lang}`, { method: 'POST' });
        return handleResponse(resp, 'Translation failed');
    },

    async translateTasksBatch(taskIds, lang) {
        const resp = await fetch('/api/tasks/translate-batch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_ids: taskIds, lang })
        });
        return handleResponse(resp, 'Batch translation failed');
    },

    async fetchArchive(params = {}) {
        const query = new URLSearchParams();
        const langParam = params.lang || 'ko';

        if (params.q) query.set('q', params.q);
        if (params.status) query.set('status', params.status);
        if (params.limit) query.set('limit', params.limit);
        if (params.offset) query.set('offset', params.offset);
        if (params.sort) query.set('sort', params.sort);
        if (params.order) query.set('order', params.order);
        query.set('lang', langParam);

        const resp = await fetch(`/api/messages/archive?${query.toString()}`);
        return handleResponse(resp, 'Fetch archive failed');
    },

    async fetchArchiveCount(q = '', status = 'all') {
        const query = new URLSearchParams();
        if (q) query.set('q', q);
        if (status) query.set('status', status);
        const resp = await fetch(`/api/messages/archive/count?${query.toString()}`);
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
            body: JSON.stringify({ aliases: original, display_name: primary })
        });
        return handleResponse(resp, 'Add tenant alias failed');
    },

    async removeTenantAlias(id) {
        const resp = await fetch('/api/tenant/alias/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ canonical_id: id })
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
            body: JSON.stringify({ display_name: repName, aliases })
        });
        return handleResponse(resp, 'Add contact mapping failed');
    },

    async removeContactMapping(id) {
        const resp = await fetch('/api/contacts/mapping/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ canonical_id: id })
        });
        return handleResponse(resp, 'Remove contact mapping failed');
    },

    async fetchReleaseNotes(type = 'user', lang = 'ko') {
        const resp = await fetch(`/api/release-notes?type=${type}&lang=${lang}`);
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
    },

    /**
     * @description Fetch all AI Weekly Reports
     */
    async fetchReports() {
        const resp = await fetch('/api/reports');
        return handleResponse(resp, 'Fetch reports failed');
    },

    /**
     * @description Generate a new AI Weekly Report for a specific period
     */
    async generateReport(startDate, endDate) {
        // Why: AI generation can take a long time. Implement a 60-second timeout using AbortController.
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 60000); // 60초 타임아웃

        try {
            const resp = await fetch(`/api/reports?start=${startDate}&end=${endDate}`, {
                method: 'POST',
                signal: controller.signal
            });
            return await handleResponse(resp, 'Generate report failed');
        } catch (err) {
            if (err.name === 'AbortError') {
                throw new Error('AI 리포트 생성 시간이 초과되었습니다 (60초). 잠시 후 다시 시도해 주세요.');
            }
            throw err;
        } finally {
            clearTimeout(timeoutId);
        }
    },

    /**
     * @description Fetch a specific AI Weekly Report by ID
     */
    async fetchReportDetail(id) {
        const resp = await fetch(`/api/reports/${id}`);
        return handleResponse(resp, 'Fetch report detail failed');
    },

    /**
     * @description Delete a specific AI Weekly Report by ID
     */
    async deleteReport(id) {
        const resp = await fetch(`/api/reports/${id}`, {
            method: 'DELETE'
        });
        return handleResponse(resp, 'Delete report failed');
    },

    /**
     * @description Request JIT translation for a specific report
     */
    async translateReport(id, lang) {
        const resp = await fetch(`/api/reports/${id}/translate?lang=${lang}`, {
            method: 'POST'
        });
        return handleResponse(resp, 'Translation request failed');
    },

    /**
     * @description Search contacts for autocomplete.
     */
    async searchContacts(q) {
        const resp = await fetch(`/api/contacts/search?q=${encodeURIComponent(q)}`);
        return handleResponse(resp, 'Search contacts failed');
    },

    /**
     * @description Link two contacts (target -> master).
     */
    async linkAccounts(targetId, masterId) {
        const resp = await fetch('/api/contacts/link', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ target_id: targetId, master_id: masterId })
        });
        return handleResponse(resp, 'Link accounts failed');
    },

    /**
     * @description Unlink a contact from its master.
     */
    async unlinkAccount(contactId) {
        const resp = await fetch('/api/contacts/unlink', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ contact_id: contactId })
        });
        return handleResponse(resp, 'Unlink account failed');
    },

    /**
     * @description Fetch all current account links for the user.
     */
    async fetchLinkedAccounts() {
        const resp = await fetch('/api/contacts/links');
        return handleResponse(resp, 'Fetch linked accounts failed');
    }
};
