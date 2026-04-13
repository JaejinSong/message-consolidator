import { apiFetch } from './utils/apiClient';
import { state, upsertReport } from './state';
import { normalizeReportData } from './logic';
import { Message, UserProfile, UserStats, TokenUsage, AchievementEntry, IReportData, AccountItem, CategorizedMessages } from './types';

/**
 * @file api.ts
 * @description Centralized API service collection in TypeScript.
 * All methods use the centralized apiFetch client from src/utils/apiClient.ts.
 */

const ensureInt = (id: string | number): number => {
    const num = Number(id);
    if (!Number.isInteger(num) || num <= 0) {
        throw new Error(`Invalid ID detected: ${id} (Parsed as: ${num}). Expected a positive integer.`);
    }
    return num;
};

const ensureIntArray = (ids: (string | number)[]): number[] => {
    if (!Array.isArray(ids)) {
        throw new Error(`Expected an array of IDs, but received: ${typeof ids}`);
    }
    return ids.map(ensureInt);
};

class TranslationBatcher {
    private queue: Map<string, Set<number>> = new Map();
    private promises: Map<string, { promise: Promise<string>; resolve: (value: string) => void; reject: (reason?: any) => void }> = new Map();
    private timer: ReturnType<typeof setTimeout> | null = null;

    request(id: string | number, lang: string): Promise<string> {
        const validatedId = ensureInt(id);
        const key = `${validatedId}_${lang}`;
        if (this.promises.has(key)) return this.promises.get(key)!.promise;

        if (!this.queue.has(lang)) this.queue.set(lang, new Set());
        this.queue.get(lang)!.add(validatedId);

        let resolve!: (value: string) => void;
        let reject!: (reason?: any) => void;
        const promise = new Promise<string>((res, rej) => { resolve = res; reject = rej; });
        this.promises.set(key, { promise, resolve, reject });

        if (!this.timer) this.timer = setTimeout(() => this.flush(), 50);
        return promise;
    }

    private async flush() {
        this.timer = null;
        const currentQueue = new Map(this.queue);
        this.queue.clear();

        for (const [lang, ids] of currentQueue) {
            this.processBatch(Array.from(ids), lang);
        }
    }

    private async processBatch(ids: number[], lang: string) {
        try {
            const { results } = await api.translateTasksBatch(ids, lang);
            ids.forEach(id => {
                const res = (results as any[]).find(r => r.id === id);
                const key = `${id}_${lang}`;
                const entry = this.promises.get(key);
                if (entry) {
                    entry.resolve(res?.translated_text || "");
                    this.promises.delete(key);
                }
            });
        } catch (err) {
            ids.forEach(id => {
                const key = `${id}_${lang}`;
                const entry = this.promises.get(key);
                if (entry) {
                    entry.reject(err);
                    this.promises.delete(key);
                }
            });
        }
    }
}

const batcher = new TranslationBatcher();

export const api = {
    async requestTranslation(id: string | number, lang: string): Promise<string> {
        return batcher.request(id, lang);
    },

    async fetchMessages(lang: string): Promise<CategorizedMessages & { messages?: CategorizedMessages, user?: UserProfile }> {
        return apiFetch('/messages', { params: { lang }, errorMessage: 'Fetch messages failed' });
    },

    async toggleDone(id: string | number, done: boolean): Promise<{ user?: UserProfile }> {
        const validatedId = ensureInt(id);
        return apiFetch('/messages/done', {
            method: 'POST',
            body: JSON.stringify({ id: validatedId, done }),
            errorMessage: 'Toggle done failed'
        });
    },

    async deleteTask(idOrIds: string | number | (string | number)[]): Promise<{ user?: UserProfile }> {
        const body = Array.isArray(idOrIds) 
            ? { ids: ensureIntArray(idOrIds) } 
            : { id: ensureInt(idOrIds) };
        return apiFetch('/messages/delete', {
            method: 'POST',
            body: JSON.stringify(body),
            errorMessage: 'Delete task failed'
        });
    },

    async hardDeleteTasks(ids: (string | number)[]): Promise<any> {
        const validatedIds = ensureIntArray(ids);
        return apiFetch('/messages/hard-delete', {
            method: 'POST',
            body: JSON.stringify({ ids: validatedIds }),
            errorMessage: 'Hard delete failed'
        });
    },

    async restoreTasks(ids: (string | number)[]): Promise<any> {
        const validatedIds = ensureIntArray(ids);
        return apiFetch('/messages/restore', {
            method: 'POST',
            body: JSON.stringify({ ids: validatedIds }),
            errorMessage: 'Restore failed'
        });
    },

    async fetchWhatsAppStatus(): Promise<{ status: string }> {
        return apiFetch('/whatsapp/status', { errorMessage: 'WA status check failed' });
    },

    async fetchSlackStatus(): Promise<{ status: string }> {
        return apiFetch('/slack/status', { errorMessage: 'Slack status check failed' });
    },

    async triggerScan(lang: string): Promise<any> {
        return apiFetch('/scan', { params: { lang }, errorMessage: 'Scan failed' });
    },

    async translateTasks(lang: string): Promise<any> {
        return apiFetch('/translate', { 
            method: 'POST', 
            params: { lang }, 
            errorMessage: 'Translation failed' 
        });
    },

    async translateTasksBatch(taskIds: number[], lang: string): Promise<{ results: any[] }> {
        return apiFetch('/tasks/translate-batch', {
            method: 'POST',
            body: JSON.stringify({ task_ids: taskIds, lang }),
            errorMessage: 'Batch translation failed'
        });
    },

    async fetchArchive(params: any = {}): Promise<Message[]> {
        const queryParams = { ...params };
        if (!queryParams.lang) queryParams.lang = 'ko';
        if (!queryParams.status) queryParams.status = 'all';

        return apiFetch('/messages/archive', { 
            params: queryParams, 
            errorMessage: 'Fetch archive failed' 
        });
    },

    async fetchArchiveCount(q = '', status = 'all'): Promise<{ count: number }> {
        const params: any = {};
        if (q) params.q = q;
        if (status) params.status = status;
        
        return apiFetch('/messages/archive/count', { 
            params, 
            errorMessage: 'Fetch archive count failed' 
        });
    },

    async fetchUserProfile(): Promise<UserProfile> {
        return apiFetch('/user/info', { errorMessage: 'User info fetch failed' });
    },

    async fetchAliases(): Promise<string[]> {
        return apiFetch('/user/aliases', { errorMessage: 'Fetch aliases failed' });
    },

    async fetchUserStats(): Promise<UserStats> {
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        return apiFetch('/user/stats', {
            headers: { 'X-Timezone': tz },
            errorMessage: 'Fetch user stats failed'
        });
    },

    async fetchAchievements(): Promise<AchievementEntry[]> {
        return apiFetch('/achievements', { errorMessage: 'Fetch achievements failed' });
    },

    async fetchUserAchievements(): Promise<any[]> {
        return apiFetch('/user/achievements', { errorMessage: 'Fetch user achievements failed' });
    },

    async addAlias(alias: string): Promise<any> {
        return apiFetch('/user/alias/add', {
            method: 'POST',
            body: JSON.stringify({ alias }),
            errorMessage: 'Add alias failed'
        });
    },

    async removeAlias(alias: string): Promise<any> {
        return apiFetch('/user/alias/delete', {
            method: 'POST',
            body: JSON.stringify({ alias }),
            errorMessage: 'Remove alias failed'
        });
    },

    async getWhatsAppQR(): Promise<{ qr: string }> {
        return apiFetch('/whatsapp/qr', { errorMessage: 'QR fetch failed' });
    },

    async logoutWhatsApp(): Promise<any> {
        return apiFetch('/whatsapp/logout', { 
            method: 'POST', 
            errorMessage: 'WhatsApp logout failed' 
        });
    },

    async fetchGmailStatus(): Promise<{ connected: boolean }> {
        return apiFetch('/gmail/status', { errorMessage: 'Gmail status check failed' });
    },

    async disconnectGmail(): Promise<any> {
        return apiFetch('/gmail/disconnect', { 
            method: 'POST', 
            errorMessage: 'Gmail disconnect failed' 
        });
    },

    async buyStreakFreeze(): Promise<any> {
        return apiFetch('/user/buy-freeze', { 
            method: 'POST', 
            errorMessage: 'Purchase failed' 
        });
    },

    async fetchTenantAliases(): Promise<AccountItem[]> {
        return apiFetch('/tenant/aliases', { errorMessage: 'Fetch tenant aliases failed' });
    },

    async addTenantAlias(original: string[], primary: string): Promise<any> {
        return apiFetch('/tenant/alias/add', {
            method: 'POST',
            body: JSON.stringify({ aliases: original, display_name: primary }),
            errorMessage: 'Add tenant alias failed'
        });
    },

    async removeTenantAlias(id: string | number): Promise<any> {
        const validatedId = ensureInt(id);
        return apiFetch('/tenant/alias/delete', {
            method: 'POST',
            body: JSON.stringify({ canonical_id: validatedId }),
            errorMessage: 'Remove tenant alias failed'
        });
    },

    async fetchTokenUsage(): Promise<TokenUsage> {
        return apiFetch('/user/token-usage', { errorMessage: 'Fetch token usage failed' });
    },

    async fetchContactMappings(): Promise<AccountItem[]> {
        return apiFetch('/contacts/mappings', { errorMessage: 'Fetch contact mappings failed' });
    },

    async addContactMapping(repName: string, aliases: string[]): Promise<any> {
        return apiFetch('/contacts/mapping/add', {
            method: 'POST',
            body: JSON.stringify({ display_name: repName, aliases }),
            errorMessage: 'Add contact mapping failed'
        });
    },

    async removeContactMapping(id: string | number): Promise<any> {
        const validatedId = ensureInt(id);
        return apiFetch('/contacts/mapping/delete', {
            method: 'POST',
            body: JSON.stringify({ canonical_id: validatedId }),
            errorMessage: 'Remove contact mapping failed'
        });
    },

    async fetchReleaseNotes(type = 'user', lang = 'ko'): Promise<any> {
        return apiFetch('/release-notes', { 
            params: { type, lang }, 
            errorMessage: 'Fetch release notes failed' 
        });
    },

    async fetchOriginalMessage(id: string | number): Promise<{ original_text: string }> {
        const validatedId = ensureInt(id);
        return apiFetch(`/messages/${validatedId}/original`, { 
            errorMessage: 'Fetch original message failed' 
        });
    },

    async getChannelStatus(): Promise<{ slack: boolean, whatsapp: boolean, gmail: boolean }> {
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

    async fetchReports(): Promise<IReportData[]> {
        return apiFetch('/reports', { errorMessage: 'Fetch reports failed' });
    },

    async fetchReportHistory(): Promise<any[]> {
        return apiFetch('/reports/history', { errorMessage: 'Fetch report history failed' });
    },

    async generateReport(startDate: string, endDate: string): Promise<IReportData> {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 120000);

        try {
            return await apiFetch('/reports', {
                method: 'POST',
                params: { start: startDate, end: endDate },
                signal: controller.signal,
                errorMessage: 'Generate report failed'
            });
        } catch (err: any) {
            if (err.name === 'AbortError') {
                throw new Error('AI 리포트 생성 시간이 초과되었습니다 (120초). 잠시 후 다시 시도해 주세요.');
            }
            throw err;
        } finally {
            clearTimeout(timeoutId);
        }
    },

    /**
     * Why: Frontend double-caching logic (State -> API).
     * Enforces YYYY-MM-DD date matching.
     */
    async getReport(date: string): Promise<IReportData> {
        if (state.reports && state.reports[date]) {
            return state.reports[date];
        }
        
        const rawReport = await this.generateReport(date, date);
        const normalized = normalizeReportData(rawReport);
        upsertReport(normalized);
        return state.reports[date];
    },

    async fetchReportDetail(id: string | number): Promise<IReportData> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}`, { errorMessage: 'Fetch report detail failed' });
    },

    async deleteReport(id: string | number): Promise<any> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}`, {
            method: 'DELETE',
            errorMessage: 'Delete report failed'
        });
    },

    async translateReport(id: string | number, lang: string): Promise<any> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}/translate`, {
            method: 'POST',
            params: { lang },
            errorMessage: 'Translation request failed'
        });
    },

    async searchContacts(q: string): Promise<AccountItem[]> {
        return apiFetch('/contacts/search', { 
            params: { q }, 
            errorMessage: 'Search contacts failed' 
        });
    },

    async linkAccounts(targetId: string | number, masterId: string | number): Promise<any> {
        const vTarget = ensureInt(targetId);
        const vMaster = ensureInt(masterId);
        return apiFetch('/contacts/link', {
            method: 'POST',
            body: JSON.stringify({ target_id: vTarget, master_id: vMaster }),
            errorMessage: 'Link accounts failed'
        });
    },

    async unlinkAccount(contactId: string | number): Promise<any> {
        const validatedId = ensureInt(contactId);
        return apiFetch('/contacts/unlink', {
            method: 'POST',
            body: JSON.stringify({ contact_id: validatedId }),
            errorMessage: 'Unlink account failed'
        });
    },

    async fetchLinkedAccounts(): Promise<any[]> {
        return apiFetch('/contacts/links', { errorMessage: 'Fetch linked accounts failed' });
    },

    async mergeTasks(targetIds: (string | number)[], destinationId: string | number): Promise<any> {
        const validatedTargets = ensureIntArray(targetIds);
        const validatedDest = ensureInt(destinationId);
        return apiFetch('/tasks/merge', {
            method: 'PUT',
            body: JSON.stringify({ target_ids: validatedTargets, destination_id: validatedDest }),
            errorMessage: 'Merge tasks failed'
        });
    }
};
