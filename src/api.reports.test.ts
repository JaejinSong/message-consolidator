import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from './api';
import { state } from './state';

describe('api — reports, contacts, identity, admin', () => {
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

    // ── reports ───────────────────────────────────────────────────────────────

    describe('fetchReports', () => {
        it('hits /reports', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.fetchReports();
            expect(fetch).toHaveBeenCalledWith('/api/reports', expect.any(Object));
        });
    });

    describe('fetchReportHistory', () => {
        it('hits /reports/history', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.fetchReportHistory();
            expect(fetch).toHaveBeenCalledWith('/api/reports/history', expect.any(Object));
        });
    });

    describe('generateReport', () => {
        it('posts with start, end, lang params', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { id: 1, start_date: '2024-01-01', end_date: '2024-01-31' }));
            await api.generateReport('2024-01-01', '2024-01-31');
            const [url, opts] = (fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
            expect(opts.method).toBe('POST');
            expect(url).toContain('start=2024-01-01');
            expect(url).toContain('end=2024-01-31');
        });

        it('throws timeout error on AbortError', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => {
                const err = new Error('aborted');
                err.name = 'AbortError';
                return Promise.reject(err);
            });
            await expect(api.generateReport('2024-01-01', '2024-01-31')).rejects.toThrow('120초');
        });

        it('re-throws non-abort errors', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(500, 'fail', 'text/plain'));
            await expect(api.generateReport('2024-01-01', '2024-01-31')).rejects.toThrow('Generate report failed');
        });
    });

    describe('getReport', () => {
        it('returns cached report without fetch when state has it', async () => {
            const cached = { id: 99, start_date: '2024-05-01', end_date: '2024-05-01' } as unknown as import('./types').IReportData;
            state.reports['2024-05-01'] = cached;
            const result = await api.getReport('2024-05-01');
            expect(fetch).not.toHaveBeenCalled();
            expect(result).toBe(cached);
            delete state.reports['2024-05-01'];
        });
    });

    describe('fetchReportDetail', () => {
        it('hits /reports/:id', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { id: 3 }));
            await api.fetchReportDetail(3);
            expect(fetch).toHaveBeenCalledWith('/api/reports/3', expect.any(Object));
        });
    });

    describe('deleteReport', () => {
        it('sends DELETE to /reports/:id', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.deleteReport(8);
            expect(fetch).toHaveBeenCalledWith('/api/reports/8', expect.objectContaining({ method: 'DELETE' }));
        });
    });

    describe('translateReport', () => {
        it('posts to /reports/:id/translate with lang param', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { translation: 'translated' }));
            await api.translateReport(5, 'en');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('/api/reports/5/translate');
            expect(url).toContain('lang=en');
        });
    });

    describe('exportReportToNotion', () => {
        it('posts to /reports/:id/export/notion', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { url: 'https://notion.so/page' }));
            const result = await api.exportReportToNotion(11);
            expect(fetch).toHaveBeenCalledWith('/api/reports/11/export/notion', expect.objectContaining({ method: 'POST' }));
            expect(result.url).toContain('notion.so');
        });
    });

    // ── contacts / identity ───────────────────────────────────────────────────

    describe('searchContacts', () => {
        it('sends q param', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.searchContacts('alice');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('/api/contacts/search');
            expect(url).toContain('q=alice');
        });
    });

    describe('generateIdentityProposals', () => {
        it('posts to /identity/proposals/generate', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'queued' }));
            await api.generateIdentityProposals();
            expect(fetch).toHaveBeenCalledWith('/api/identity/proposals/generate', expect.objectContaining({ method: 'POST' }));
        });
    });

    describe('getProposalJobStatus', () => {
        it('hits /identity/proposals/job-status', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'done', proposals_created: 3 }));
            const result = await api.getProposalJobStatus();
            expect(fetch).toHaveBeenCalledWith('/api/identity/proposals/job-status', expect.any(Object));
            expect(result.proposals_created).toBe(3);
        });
    });

    describe('fetchIdentityProposals', () => {
        it('hits /identity/proposals', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.fetchIdentityProposals();
            expect(fetch).toHaveBeenCalledWith('/api/identity/proposals', expect.any(Object));
        });
    });

    describe('acceptIdentityProposal', () => {
        it('posts canonical_name to /identity/proposals/:id/accept', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.acceptIdentityProposal('grp-1', 'Alice Smith');
            expect(fetch).toHaveBeenCalledWith('/api/identity/proposals/grp-1/accept', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ canonical_name: 'Alice Smith' })
            }));
        });
    });

    describe('rejectIdentityProposal', () => {
        it('posts to /identity/proposals/:id/reject', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.rejectIdentityProposal('grp-2');
            expect(fetch).toHaveBeenCalledWith('/api/identity/proposals/grp-2/reject', expect.objectContaining({ method: 'POST' }));
        });
    });

    // ── admin ─────────────────────────────────────────────────────────────────

    describe('invalidateCache', () => {
        it('posts to /admin/invalidate-cache', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.invalidateCache();
            expect(fetch).toHaveBeenCalledWith('/api/admin/invalidate-cache', expect.objectContaining({ method: 'POST' }));
        });
    });

    describe('fetchAdminSettings', () => {
        it('hits /admin/settings', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.fetchAdminSettings();
            expect(fetch).toHaveBeenCalledWith('/api/admin/settings', expect.any(Object));
        });
    });

    describe('updateAdminSetting', () => {
        it('puts to /admin/settings/:key with value body', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.updateAdminSetting('feature_flag', 'true');
            expect(fetch).toHaveBeenCalledWith('/api/admin/settings/feature_flag', expect.objectContaining({
                method: 'PUT',
                body: JSON.stringify({ value: 'true' })
            }));
        });

        it('URL-encodes the key', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.updateAdminSetting('my key/special', 'val');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('my%20key%2Fspecial');
        });
    });

    describe('fetchAdmins', () => {
        it('hits /admin/admins', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, []));
            await api.fetchAdmins();
            expect(fetch).toHaveBeenCalledWith('/api/admin/admins', expect.any(Object));
        });
    });

    describe('addAdmin', () => {
        it('posts email to /admin/admins', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.addAdmin('admin@example.com');
            expect(fetch).toHaveBeenCalledWith('/api/admin/admins', expect.objectContaining({
                method: 'POST',
                body: JSON.stringify({ email: 'admin@example.com' })
            }));
        });
    });

    describe('removeAdmin', () => {
        it('deletes to /admin/admins/:email (URL-encoded)', async () => {
            (fetch as ReturnType<typeof vi.fn>).mockImplementation(() => mockResponse(200, { status: 'ok' }));
            await api.removeAdmin('admin@example.com');
            const url = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
            expect(url).toContain('/api/admin/admins/admin%40example.com');
            expect(fetch).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ method: 'DELETE' }));
        });
    });
});
