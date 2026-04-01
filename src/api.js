import { apiFetch } from './utils/apiClient';
import { state } from './state.ts';

/**
 * @file api.js
 * @description Centralized API service collection.
 * All methods now use the centralized apiFetch client from src/utils/apiClient.ts.
 * Path prefixes (/api) are managed by the VITE_API_BASE_URL environment variable.
 */

/**
 * Validates and converts an ID to a proper integer.
 * @param {any} id - The ID to validate and convert.
 * @returns {number} - The validated integer.
 * @throws {Error} - If the ID is invalid (not a positive integer).
 */
const ensureInt = (id) => {
    const num = Number(id);
    if (!Number.isInteger(num) || num <= 0) {
        throw new Error(`Invalid ID detected: ${id} (Parsed as: ${num}). Expected a positive integer.`);
    }
    return num;
};

/**
 * Validates and converts an array of IDs to proper integers.
 * @param {any[]} ids - The array of IDs to validate and convert.
 * @returns {number[]} - The validated integer array.
 * @throws {Error} - If any ID in the array is invalid.
 */
const ensureIntArray = (ids) => {
    if (!Array.isArray(ids)) {
        throw new Error(`Expected an array of IDs, but received: ${typeof ids}`);
    }
    return ids.map(ensureInt);
};

export const api = {
    async fetchMessages(lang) {
        return apiFetch('/messages', { params: { lang }, errorMessage: 'Fetch messages failed' });
    },

    async toggleDone(id, done) {
        const validatedId = ensureInt(id);
        return apiFetch('/messages/done', {
            method: 'POST',
            body: JSON.stringify({ id: validatedId, done }),
            errorMessage: 'Toggle done failed'
        });
    },

    async deleteTask(idOrIds) {
        const body = Array.isArray(idOrIds) 
            ? { ids: ensureIntArray(idOrIds) } 
            : { id: ensureInt(idOrIds) };
        return apiFetch('/messages/delete', {
            method: 'POST',
            body: JSON.stringify(body),
            errorMessage: 'Delete task failed'
        });
    },

    async hardDeleteTasks(ids) {
        const validatedIds = ensureIntArray(ids);
        return apiFetch('/messages/hard-delete', {
            method: 'POST',
            body: JSON.stringify({ ids: validatedIds }),
            errorMessage: 'Hard delete failed'
        });
    },

    async restoreTasks(ids) {
        const validatedIds = ensureIntArray(ids);
        return apiFetch('/messages/restore', {
            method: 'POST',
            body: JSON.stringify({ ids: validatedIds }),
            errorMessage: 'Restore failed'
        });
    },

    async fetchWhatsAppStatus() {
        return apiFetch('/whatsapp/status', { errorMessage: 'WA status check failed' });
    },

    async fetchSlackStatus() {
        return apiFetch('/slack/status', { errorMessage: 'Slack status check failed' });
    },

    async triggerScan(lang) {
        return apiFetch('/scan', { params: { lang }, errorMessage: 'Scan failed' });
    },

    async translateTasks(lang) {
        return apiFetch('/translate', { 
            method: 'POST', 
            params: { lang }, 
            errorMessage: 'Translation failed' 
        });
    },

    async translateTasksBatch(taskIds, lang) {
        const validatedIds = ensureIntArray(taskIds);
        return apiFetch('/tasks/translate-batch', {
            method: 'POST',
            body: JSON.stringify({ task_ids: validatedIds, lang }),
            errorMessage: 'Batch translation failed'
        });
    },

    async fetchArchive(params = {}) {
        const queryParams = { ...params };
        if (!queryParams.lang) queryParams.lang = 'ko';
        if (!queryParams.status) queryParams.status = 'all';

        return apiFetch('/messages/archive', { 
            params: queryParams, 
            errorMessage: 'Fetch archive failed' 
        });
    },

    async fetchArchiveCount(q = '', status = 'all') {
        const params = {};
        if (q) params.q = q;
        if (status) params.status = status;
        
        return apiFetch('/messages/archive/count', { 
            params, 
            errorMessage: 'Fetch archive count failed' 
        });
    },

    async fetchUserProfile() {
        return apiFetch('/user/info', { errorMessage: 'User info fetch failed' });
    },

    async fetchAliases() {
        return apiFetch('/user/aliases', { errorMessage: 'Fetch aliases failed' });
    },

    async fetchUserStats() {
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        return apiFetch('/user/stats', {
            headers: { 'X-Timezone': tz },
            errorMessage: 'Fetch user stats failed'
        });
    },

    async fetchAchievements() {
        return apiFetch('/achievements', { errorMessage: 'Fetch achievements failed' });
    },

    async fetchUserAchievements() {
        return apiFetch('/user/achievements', { errorMessage: 'Fetch user achievements failed' });
    },

    async addAlias(alias) {
        return apiFetch('/user/alias/add', {
            method: 'POST',
            body: JSON.stringify({ alias }),
            errorMessage: 'Add alias failed'
        });
    },

    async removeAlias(alias) {
        return apiFetch('/user/alias/delete', {
            method: 'POST',
            body: JSON.stringify({ alias }),
            errorMessage: 'Remove alias failed'
        });
    },

    async getWhatsAppQR() {
        return apiFetch('/whatsapp/qr', { errorMessage: 'QR fetch failed' });
    },

    async logoutWhatsApp() {
        return apiFetch('/whatsapp/logout', { 
            method: 'POST', 
            errorMessage: 'WhatsApp logout failed' 
        });
    },

    async fetchGmailStatus() {
        return apiFetch('/gmail/status', { errorMessage: 'Gmail status check failed' });
    },

    async disconnectGmail() {
        return apiFetch('/gmail/disconnect', { 
            method: 'POST', 
            errorMessage: 'Gmail disconnect failed' 
        });
    },

    async buyStreakFreeze() {
        return apiFetch('/user/buy-freeze', { 
            method: 'POST', 
            errorMessage: 'Purchase failed' 
        });
    },

    async fetchTenantAliases() {
        return apiFetch('/tenant/aliases', { errorMessage: 'Fetch tenant aliases failed' });
    },

    async addTenantAlias(original, primary) {
        return apiFetch('/tenant/alias/add', {
            method: 'POST',
            body: JSON.stringify({ aliases: original, display_name: primary }),
            errorMessage: 'Add tenant alias failed'
        });
    },

    async removeTenantAlias(id) {
        return apiFetch('/tenant/alias/delete', {
            method: 'POST',
            body: JSON.stringify({ canonical_id: id }),
            errorMessage: 'Remove tenant alias failed'
        });
    },

    async fetchTokenUsage() {
        return apiFetch('/user/token-usage', { errorMessage: 'Fetch token usage failed' });
    },

    async fetchContactMappings() {
        return apiFetch('/contacts/mappings', { errorMessage: 'Fetch contact mappings failed' });
    },

    async addContactMapping(repName, aliases) {
        return apiFetch('/contacts/mapping/add', {
            method: 'POST',
            body: JSON.stringify({ display_name: repName, aliases }),
            errorMessage: 'Add contact mapping failed'
        });
    },

    async removeContactMapping(id) {
        return apiFetch('/contacts/mapping/delete', {
            method: 'POST',
            body: JSON.stringify({ canonical_id: id }),
            errorMessage: 'Remove contact mapping failed'
        });
    },

    async fetchReleaseNotes(type = 'user', lang = 'ko') {
        return apiFetch('/release-notes', { 
            params: { type, lang }, 
            errorMessage: 'Fetch release notes failed' 
        });
    },

    async fetchOriginalMessage(id) {
        const validatedId = ensureInt(id);
        return apiFetch(`/messages/${validatedId}/original`, { 
            errorMessage: 'Fetch original message failed' 
        });
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
        return apiFetch('/reports', { errorMessage: 'Fetch reports failed' });
    },

    /**
     * @description Generate a new AI Weekly Report for a specific period
     */
    async generateReport(startDate, endDate) {
        // Why: AI generation can take a long time. Implement a 60-second timeout using AbortController.
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 60000);

        try {
            return await apiFetch('/reports', {
                method: 'POST',
                params: { start: startDate, end: endDate },
                signal: controller.signal,
                errorMessage: 'Generate report failed'
            });
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
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}`, { errorMessage: 'Fetch report detail failed' });
    },

    /**
     * @description Delete a specific AI Weekly Report by ID
     */
    async deleteReport(id) {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}`, {
            method: 'DELETE',
            errorMessage: 'Delete report failed'
        });
    },

    /**
     * @description Request JIT translation for a specific report
     */
    async translateReport(id, lang) {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}/translate`, {
            method: 'POST',
            params: { lang },
            errorMessage: 'Translation request failed'
        });
    },

    /**
     * @description Search contacts for autocomplete.
     */
    async searchContacts(q) {
        return apiFetch('/contacts/search', { 
            params: { q }, 
            errorMessage: 'Search contacts failed' 
        });
    },

    /**
     * @description Link two contacts (target -> master).
     */
    async linkAccounts(targetId, masterId) {
        const vTarget = ensureInt(targetId);
        const vMaster = ensureInt(masterId);
        return apiFetch('/contacts/link', {
            method: 'POST',
            body: JSON.stringify({ target_id: vTarget, master_id: vMaster }),
            errorMessage: 'Link accounts failed'
        });
    },

    /**
     * @description Unlink a contact from its master.
     */
    async unlinkAccount(contactId) {
        const validatedId = ensureInt(contactId);
        return apiFetch('/contacts/unlink', {
            method: 'POST',
            body: JSON.stringify({ contact_id: validatedId }),
            errorMessage: 'Unlink account failed'
        });
    },

    /**
     * @description Fetch all current account links for the user.
     */
    async fetchLinkedAccounts() {
        return apiFetch('/contacts/links', { errorMessage: 'Fetch linked accounts failed' });
    }
};
