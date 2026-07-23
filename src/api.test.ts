import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from './api';

describe('api', () => {
    beforeEach(() => {
        (fetch as ReturnType<typeof vi.fn>).mockClear();
    });

    const mockResponse = (status: number, data: unknown, contentType = 'application/json') => {
        return Promise.resolve({
            status,
            ok: status >= 200 && status < 300,
            headers: new Map([['content-type', contentType]]),
            json: () => Promise.resolve(data),
            text: () => Promise.resolve(typeof data === 'string' ? data : JSON.stringify(data))
        });
    };

    // ── messages ──────────────────────────────────────────────────────────────

    describe('fetchMessages', () => {
        it('fetches with correct language parameter', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, [{ id: 1 }]));
            const data = await api.fetchMessages('ko');
            expect(fetch).toHaveBeenCalledWith('/api/messages?lang=ko', expect.any(Object));
            expect((data as unknown as Array<{ id: number }>)[0].id).toBe(1);
        });

        it('rejects on 401', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(401, { error: 'Unauthorized' }));
            await expect(api.fetchMessages('ko')).rejects.toThrow('Unauthorized');
        });

        it('rejects on non-JSON 500', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(500, 'Server Error', 'text/plain'));
            await expect(api.fetchMessages('ko')).rejects.toThrow('Fetch messages failed');
        });

        it('rejects on network error', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => Promise.reject(new TypeError('Failed to fetch')));
            await expect(api.fetchMessages('ko')).rejects.toThrow('Failed to fetch');
        });

        it('rejects on text/html response (session expired)', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, '<html>login</html>', 'text/html'));
            await expect(api.fetchMessages('ko')).rejects.toThrow('Authentication Required (Session Expired or Proxy Redirect)');
        });
    });

    describe('searchActiveMessages', () => {
        it('sends q and lang as query params', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { inbox: [], delegated: [], reference: [] }));
            await api.searchActiveMessages('hello', 'en');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('/api/messages/search');
            expect(url).toContain('q=hello');
            expect(url).toContain('lang=en');
        });
    });

    // ── task mutations ────────────────────────────────────────────────────────

    describe('toggleDone', () => {
        it('sends correct body', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { success: true }));
            await api.toggleDone(123, true);
            expect(fetch).toHaveBeenCalledWith('/api/messages/done', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ id: 123, done: true })
            }));
        });

        it('rejects on invalid id (string non-numeric)', async () => {
            await expect(api.toggleDone('abc', true)).rejects.toThrow('Invalid ID');
        });
    });

    describe('toggleSubtask', () => {
        it('sends id, subtask_index, done in body', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, {}));
            await api.toggleSubtask(5, 2, false);
            expect(fetch).toHaveBeenCalledWith('/api/subtasks/toggle', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ id: 5, subtask_index: 2, done: false })
            }));
        });
    });

    describe('deleteTask', () => {
        it('sends single id when scalar provided', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, {}));
            await api.deleteTask(7);
            expect(fetch).toHaveBeenCalledWith('/api/messages/delete', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ id: 7 })
            }));
        });

        it('sends ids array when array provided', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, {}));
            await api.deleteTask([1, 2, 3]);
            expect(fetch).toHaveBeenCalledWith('/api/messages/delete', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ ids: [1, 2, 3] })
            }));
        });
    });

    describe('hardDeleteTasks', () => {
        it('posts validated ids', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.hardDeleteTasks([10, 11]);
            expect(fetch).toHaveBeenCalledWith('/api/messages/hard-delete', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ ids: [10, 11] })
            }));
        });
    });

    describe('restoreTasks', () => {
        it('posts to restore endpoint', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.restoreTasks([4, 5]);
            expect(fetch).toHaveBeenCalledWith('/api/messages/restore', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ ids: [4, 5] })
            }));
        });
    });

    describe('mergeTasks', () => {
        it('sends target_ids and destination_id via PUT', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.mergeTasks([1, 2], 3);
            expect(fetch).toHaveBeenCalledWith('/api/tasks/merge', expect.objectContaining({
                method: 'PUT',
                body: JSON.stringify({ target_ids: [1, 2], destination_id: 3 })
            }));
        });
    });

    // ── archive ───────────────────────────────────────────────────────────────

    describe('fetchArchive', () => {
        it('uses default lang=ko and status=all when omitted', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { total: 0, messages: [] }));
            await api.fetchArchive();
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('lang=ko');
            expect(url).toContain('status=all');
        });

        it('passes through all provided params', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { total: 0, messages: [] }));
            await api.fetchArchive({ q: 'test', limit: 10, offset: 20, lang: 'en', sort: 'time', order: 'ASC' });
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('q=test');
            expect(url).toContain('limit=10');
            expect(url).toContain('offset=20');
            expect(url).toContain('lang=en');
            expect(url).toContain('sort=time');
            expect(url).toContain('order=ASC');
        });
    });

    describe('fetchArchiveCount', () => {
        it('includes q and status when provided', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { count: 5 }));
            await api.fetchArchiveCount('search', 'done');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('q=search');
            expect(url).toContain('status=done');
        });

        it('omits q param when empty string', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { count: 0 }));
            await api.fetchArchiveCount('', 'all');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).not.toContain('q=');
        });
    });

    // ── status checks ─────────────────────────────────────────────────────────

    describe('fetchWhatsAppStatus', () => {
        it('hits /whatsapp/status and returns data', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'connected', device_name: 'phone' }));
            const result = await api.fetchWhatsAppStatus();
            expect(fetch).toHaveBeenCalledWith('/api/whatsapp/status', expect.any(Object));
            expect(result.status).toBe('connected');
        });
    });

    describe('fetchSlackStatus', () => {
        it('hits /slack/status', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'connected' }));
            await api.fetchSlackStatus();
            expect(fetch).toHaveBeenCalledWith('/api/slack/status', expect.any(Object));
        });
    });

    describe('fetchGmailStatus', () => {
        it('hits /gmail/status', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { connected: true }));
            const result = await api.fetchGmailStatus();
            expect(fetch).toHaveBeenCalledWith('/api/gmail/status', expect.any(Object));
            expect(result.connected).toBe(true);
        });

        it('passes through scan freshness fields', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { connected: true, last_scan_at: 1753200000, stale: true }));
            const result = await api.fetchGmailStatus();
            expect(result.stale).toBe(true);
            expect(result.last_scan_at).toBe(1753200000);
        });
    });

    describe('fetchTelegramStatus', () => {
        it('hits /telegram/status', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'disconnected' }));
            await api.fetchTelegramStatus();
            expect(fetch).toHaveBeenCalledWith('/api/telegram/status', expect.any(Object));
        });
    });

    describe('getChannelStatus', () => {
        it('aggregates all four channel statuses', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
                if ((url as string).includes('slack')) return mockResponse(200, { status: 'connected' });
                if ((url as string).includes('whatsapp')) return mockResponse(200, { status: 'connected' });
                if ((url as string).includes('gmail')) return mockResponse(200, { connected: true });
                if ((url as string).includes('telegram')) return mockResponse(200, { status: 'disconnected' });
                return mockResponse(200, {});
            });
            const result = await api.getChannelStatus();
            expect(result.slack).toBe(true);
            expect(result.whatsapp).toBe(true);
            expect(result.gmail).toBe(true);
            expect(result.telegram).toBe(false);
        });

        it('returns false for any channel that errors', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => Promise.reject(new Error('network')));
            const result = await api.getChannelStatus();
            expect(result.slack).toBe(false);
            expect(result.whatsapp).toBe(false);
            expect(result.gmail).toBe(false);
            expect(result.telegram).toBe(false);
        });
    });

    // ── scan / translate ──────────────────────────────────────────────────────

    describe('triggerScan', () => {
        it('sends lang param', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.triggerScan('en');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('/api/scan');
            expect(url).toContain('lang=en');
        });
    });

    describe('translateTasks', () => {
        it('uses POST with lang param', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.translateTasks('ko');
            const [url, opts] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
            expect(url).toContain('lang=ko');
            expect(opts.method).toBe('POST');
        });
    });

    describe('translateTasksBatch', () => {
        it('posts task_ids and lang', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { results: [{ id: 1, translated_text: 'translated' }] }));
            const result = await api.translateTasksBatch([1, 2], 'en');
            expect(fetch).toHaveBeenCalledWith('/api/tasks/translate-batch', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ task_ids: [1, 2], lang: 'en' })
            }));
            expect(result.results[0].translated_text).toBe('translated');
        });
    });

    // ── user ──────────────────────────────────────────────────────────────────

    describe('fetchUserProfile', () => {
        it('hits /user/info', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { email: 'a@b.com', name: 'Test', picture: '', points: 0, streak: 0, streak_freezes: 0 }));
            const result = await api.fetchUserProfile();
            expect(fetch).toHaveBeenCalledWith('/api/user/info', expect.any(Object));
            expect(result.email).toBe('a@b.com');
        });
    });

    describe('fetchUserStats', () => {
        it('hits /user/stats with X-Timezone header', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { total: 10 }));
            await api.fetchUserStats();
            const [url, opts] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
            expect(url).toContain('/api/user/stats');
            expect((opts.headers as Record<string, string>)['X-Timezone']).toBeTruthy();
        });
    });

    describe('fetchTokenUsage', () => {
        it('hits /user/token-usage', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { used: 100, limit: 1000 }));
            await api.fetchTokenUsage();
            expect(fetch).toHaveBeenCalledWith('/api/user/token-usage', expect.any(Object));
        });
    });

    describe('buyStreakFreeze', () => {
        it('uses POST to /user/buy-freeze', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.buyStreakFreeze();
            expect(fetch).toHaveBeenCalledWith('/api/user/buy-freeze', expect.objectContaining({ method: 'POST' }));
        });
    });

    // ── WhatsApp ──────────────────────────────────────────────────────────────

    describe('getWhatsAppQR', () => {
        it('hits /whatsapp/qr', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { qr: 'data:image/png;base64,abc' }));
            const result = await api.getWhatsAppQR();
            expect(fetch).toHaveBeenCalledWith('/api/whatsapp/qr', expect.any(Object));
            expect(result.qr).toContain('data:image');
        });
    });

    describe('logoutWhatsApp', () => {
        it('posts to /whatsapp/logout', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.logoutWhatsApp();
            expect(fetch).toHaveBeenCalledWith('/api/whatsapp/logout', expect.objectContaining({ method: 'POST' }));
        });
    });

    // ── Gmail ─────────────────────────────────────────────────────────────────

    describe('disconnectGmail', () => {
        it('posts to /gmail/disconnect', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.disconnectGmail();
            expect(fetch).toHaveBeenCalledWith('/api/gmail/disconnect', expect.objectContaining({ method: 'POST' }));
        });
    });

    // ── Telegram ──────────────────────────────────────────────────────────────

    describe('saveTelegramCredentials', () => {
        it('posts app_id and app_hash', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.saveTelegramCredentials(12345, 'hash_abc');
            expect(fetch).toHaveBeenCalledWith('/api/telegram/credentials', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ app_id: 12345, app_hash: 'hash_abc' })
            }));
        });
    });

    describe('startTelegramAuth', () => {
        it('posts phone number', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'code_sent' }));
            await api.startTelegramAuth('+821012345678');
            expect(fetch).toHaveBeenCalledWith('/api/telegram/auth/start', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ phone: '+821012345678' })
            }));
        });
    });

    describe('confirmTelegramCode', () => {
        it('posts code', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.confirmTelegramCode('12345');
            expect(fetch).toHaveBeenCalledWith('/api/telegram/auth/confirm', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ code: '12345' })
            }));
        });
    });

    describe('confirmTelegramPassword', () => {
        it('posts password', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.confirmTelegramPassword('secret');
            expect(fetch).toHaveBeenCalledWith('/api/telegram/auth/password', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ password: 'secret' })
            }));
        });
    });

    describe('logoutTelegram', () => {
        it('posts to /telegram/logout', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.logoutTelegram();
            expect(fetch).toHaveBeenCalledWith('/api/telegram/logout', expect.objectContaining({ method: 'POST' }));
        });
    });

    // ── release notes ─────────────────────────────────────────────────────────

    describe('fetchReleaseNotes', () => {
        it('uses default type=user, lang=ko', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { content: 'notes' }));
            await api.fetchReleaseNotes();
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('type=user');
            expect(url).toContain('lang=ko');
        });

        it('passes overridden type and lang', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { content: 'admin notes' }));
            await api.fetchReleaseNotes('admin', 'en');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('type=admin');
            expect(url).toContain('lang=en');
        });
    });

    // ── original message ──────────────────────────────────────────────────────

    describe('fetchOriginalMessage', () => {
        it('uses dynamic id in URL', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { original_text: 'raw' }));
            const result = await api.fetchOriginalMessage(42);
            expect(fetch).toHaveBeenCalledWith('/api/messages/42/original', expect.any(Object));
            expect(result.original_text).toBe('raw');
        });

        it('rejects on invalid id', async () => {
            await expect(api.fetchOriginalMessage('abc')).rejects.toThrow('Invalid ID');
        });
    });

});
