import { apiFetch } from './utils/http';
import { state, upsertReport } from './state';
import { normalizeReportData } from './logic';
import { Message, UserProfile, UserStats, TokenUsage, IReportData, AccountItem, CategorizedMessages, TranslateBatchResult } from './types';
import type { ProposalGroup } from './renderers/settings-renderer';

// Why: shared shape for mutation endpoints that the backend answers with `{ status: "ok" }` or
// `{ status, user }` — keeps the per-method generic narrow without losing the optional user echo
// some toggles return for cheap stat refresh.
export interface ApiStatusResponse {
    status?: string;
    user?: UserProfile;
}

/**
 * @file api.ts
 * @description Centralized API service collection in TypeScript.
 * All methods use the centralized apiFetch client from src/utils/http.ts.
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
    private promises: Map<string, { promise: Promise<string>; resolve: (value: string) => void; reject: (reason?: unknown) => void }> = new Map();
    private timer: ReturnType<typeof setTimeout> | null = null;

    request(id: string | number, lang: string): Promise<string> {
        const validatedId = ensureInt(id);
        const key = `${validatedId}_${lang}`;
        if (this.promises.has(key)) return this.promises.get(key)!.promise;

        if (!this.queue.has(lang)) this.queue.set(lang, new Set());
        this.queue.get(lang)!.add(validatedId);

        let resolve!: (value: string) => void;
        let reject!: (reason?: unknown) => void;
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
                const res = results.find(r => r.id === id);
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

    async toggleSubtask(id: string | number, subtaskIndex: number, done: boolean): Promise<{ user?: UserProfile }> {
        const validatedId = ensureInt(id);
        return apiFetch('/subtasks/toggle', {
            method: 'POST',
            body: JSON.stringify({ id: validatedId, subtask_index: subtaskIndex, done }),
            errorMessage: 'Toggle subtask failed'
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

    async hardDeleteTasks(ids: (string | number)[]): Promise<ApiStatusResponse> {
        const validatedIds = ensureIntArray(ids);
        return apiFetch('/messages/hard-delete', {
            method: 'POST',
            body: JSON.stringify({ ids: validatedIds }),
            errorMessage: 'Hard delete failed'
        });
    },

    async restoreTasks(ids: (string | number)[]): Promise<ApiStatusResponse> {
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

    async triggerScan(lang: string): Promise<ApiStatusResponse> {
        return apiFetch('/scan', { params: { lang }, errorMessage: 'Scan failed' });
    },

    async translateTasks(lang: string): Promise<ApiStatusResponse> {
        return apiFetch('/translate', {
            method: 'POST',
            params: { lang },
            errorMessage: 'Translation failed'
        });
    },

    async translateTasksBatch(taskIds: number[], lang: string): Promise<{ results: TranslateBatchResult[] }> {
        return apiFetch('/tasks/translate-batch', {
            method: 'POST',
            body: JSON.stringify({ task_ids: taskIds, lang }),
            errorMessage: 'Batch translation failed'
        });
    },

    async fetchArchive(params: Record<string, string | number | boolean | undefined> = {}): Promise<{ total: number; messages: Message[] }> {
        const queryParams = { ...params };
        if (!queryParams.lang) queryParams.lang = 'ko';
        if (!queryParams.status) queryParams.status = 'all';

        return apiFetch('/messages/archive', {
            params: queryParams,
            errorMessage: 'Fetch archive failed'
        });
    },

    async fetchArchiveCount(q = '', status = 'all'): Promise<{ count: number }> {
        const params: Record<string, string | undefined> = {};
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

    async fetchUserStats(): Promise<UserStats> {
        const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
        return apiFetch('/user/stats', {
            headers: { 'X-Timezone': tz },
            errorMessage: 'Fetch user stats failed'
        });
    },

    async getWhatsAppQR(): Promise<{ qr: string }> {
        return apiFetch('/whatsapp/qr', { errorMessage: 'QR fetch failed' });
    },

    async logoutWhatsApp(): Promise<ApiStatusResponse> {
        return apiFetch('/whatsapp/logout', {
            method: 'POST',
            errorMessage: 'WhatsApp logout failed'
        });
    },

    async fetchGmailStatus(): Promise<{ connected: boolean }> {
        return apiFetch('/gmail/status', { errorMessage: 'Gmail status check failed' });
    },

    async disconnectGmail(): Promise<ApiStatusResponse> {
        return apiFetch('/gmail/disconnect', {
            method: 'POST',
            errorMessage: 'Gmail disconnect failed'
        });
    },

    async fetchTelegramStatus(): Promise<{ status: string; has_credentials?: boolean }> {
        return apiFetch('/telegram/status', { errorMessage: 'Telegram status check failed' });
    },

    async saveTelegramCredentials(appId: number, appHash: string): Promise<{ status: string }> {
        return apiFetch('/telegram/credentials', {
            method: 'POST',
            body: JSON.stringify({ app_id: appId, app_hash: appHash }),
            errorMessage: 'Telegram credentials save failed'
        });
    },

    async startTelegramAuth(phone: string): Promise<{ status: string }> {
        return apiFetch('/telegram/auth/start', {
            method: 'POST',
            body: JSON.stringify({ phone }),
            errorMessage: 'Telegram auth start failed'
        });
    },

    async confirmTelegramCode(code: string): Promise<{ status: string }> {
        return apiFetch('/telegram/auth/confirm', {
            method: 'POST',
            body: JSON.stringify({ code }),
            errorMessage: 'Telegram code confirmation failed'
        });
    },

    async confirmTelegramPassword(password: string): Promise<{ status: string }> {
        return apiFetch('/telegram/auth/password', {
            method: 'POST',
            body: JSON.stringify({ password }),
            errorMessage: 'Telegram password confirmation failed'
        });
    },

    async logoutTelegram(): Promise<{ status: string }> {
        return apiFetch('/telegram/logout', {
            method: 'POST',
            errorMessage: 'Telegram logout failed'
        });
    },

    async buyStreakFreeze(): Promise<ApiStatusResponse> {
        return apiFetch('/user/buy-freeze', {
            method: 'POST',
            errorMessage: 'Purchase failed'
        });
    },

    async fetchTokenUsage(): Promise<TokenUsage> {
        return apiFetch('/user/token-usage', { errorMessage: 'Fetch token usage failed' });
    },

    async fetchReleaseNotes(type = 'user', lang = 'ko'): Promise<{ content?: string }> {
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

    async getChannelStatus(): Promise<{ slack: boolean, whatsapp: boolean, gmail: boolean, telegram: boolean }> {
        const [slack, whatsapp, gmail, telegram] = await Promise.all([
            this.fetchSlackStatus().catch(() => ({ status: 'DISCONNECTED' })),
            this.fetchWhatsAppStatus().catch(() => ({ status: 'DISCONNECTED' })),
            this.fetchGmailStatus().catch(() => ({ connected: false })),
            this.fetchTelegramStatus().catch(() => ({ status: 'disconnected' }))
        ]);

        return {
            slack: slack?.status === 'CONNECTED',
            whatsapp: whatsapp?.status === 'CONNECTED',
            gmail: gmail?.connected === true,
            telegram: telegram?.status === 'connected'
        };
    },

    async fetchReports(): Promise<IReportData[]> {
        return apiFetch('/reports', { errorMessage: 'Fetch reports failed' });
    },

    async fetchReportHistory(): Promise<IReportData[]> {
        return apiFetch('/reports/history', { errorMessage: 'Fetch report history failed' });
    },

    async generateReport(startDate: string, endDate: string, channelId?: string, status?: string): Promise<IReportData> {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 120000);

        try {
            return await apiFetch('/reports', {
                method: 'POST',
                params: { 
                    start: startDate, 
                    end: endDate, 
                    lang: state.currentLang || 'en',
                    channelId: channelId || '',
                    status: status || ''
                },
                signal: controller.signal,
                errorMessage: 'Generate report failed'
            });
        } catch (err: unknown) {
            if (err instanceof Error && err.name === 'AbortError') {
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

    async deleteReport(id: string | number): Promise<ApiStatusResponse> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}`, {
            method: 'DELETE',
            errorMessage: 'Delete report failed'
        });
    },

    async translateReport(id: string | number, lang: string): Promise<{
        report_summary?: string;
        summary?: string;
        translation?: string;
        translated_text?: string;
    }> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}/translate`, {
            method: 'POST',
            params: { lang },
            errorMessage: 'Translation request failed'
        });
    },

    async exportReportToNotion(id: string | number): Promise<{ url?: string; error?: string }> {
        const validatedId = ensureInt(id);
        return apiFetch(`/reports/${validatedId}/export/notion`, {
            method: 'POST',
            errorMessage: 'Notion export failed'
        });
    },

    async searchContacts(q: string): Promise<AccountItem[]> {
        return apiFetch('/contacts/search', { 
            params: { q }, 
            errorMessage: 'Search contacts failed' 
        });
    },

    async generateIdentityProposals(): Promise<{ status: string }> {
        return apiFetch('/identity/proposals/generate', { method: 'POST', errorMessage: 'Generate proposals failed' });
    },

    async getProposalJobStatus(): Promise<{ status: string; proposals_created?: number; error?: string }> {
        return apiFetch('/identity/proposals/job-status', { errorMessage: 'Failed to get job status' });
    },

    async fetchIdentityProposals(): Promise<ProposalGroup[]> {
        return apiFetch('/identity/proposals', { errorMessage: 'Fetch proposals failed' });
    },

    async acceptIdentityProposal(groupId: string, canonicalName: string): Promise<ApiStatusResponse> {
        return apiFetch(`/identity/proposals/${groupId}/accept`, {
            method: 'POST',
            body: JSON.stringify({ canonical_name: canonicalName }),
            errorMessage: 'Accept proposal failed'
        });
    },

    async rejectIdentityProposal(groupId: string): Promise<ApiStatusResponse> {
        return apiFetch(`/identity/proposals/${groupId}/reject`, { method: 'POST', errorMessage: 'Reject proposal failed' });
    },

    async mergeTasks(targetIds: (string | number)[], destinationId: string | number): Promise<ApiStatusResponse> {
        const validatedTargets = ensureIntArray(targetIds);
        const validatedDest = ensureInt(destinationId);
        return apiFetch('/tasks/merge', {
            method: 'PUT',
            body: JSON.stringify({ target_ids: validatedTargets, destination_id: validatedDest }),
            errorMessage: 'Merge tasks failed'
        });
    },

    invalidateCache(): Promise<{ status: string }> {
        return apiFetch('/admin/invalidate-cache', { method: 'POST', errorMessage: 'Cache invalidation failed' });
    }
};
