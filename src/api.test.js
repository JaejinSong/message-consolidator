import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from './api.js';

describe('api.js', () => {
    beforeEach(() => {
        fetch.mockClear();
    });

    const mockResponse = (status, data, contentType = 'application/json') => {
        return Promise.resolve({
            status,
            ok: status >= 200 && status < 300,
            headers: new Map([['content-type', contentType]]),
            json: () => Promise.resolve(data),
            text: () => Promise.resolve(typeof data === 'string' ? data : JSON.stringify(data))
        });
    };

    it('should fetch messages with correct language parameter', async () => {
        fetch.mockImplementation(() => mockResponse(200, [{ id: 1 }]));
        const data = await api.fetchMessages('ko');
        expect(fetch).toHaveBeenCalledWith('/api/messages?lang=ko', expect.any(Object));
        expect(data[0].id).toBe(1);
    });

    it('should handle 401 Unauthorized error', async () => {
        fetch.mockImplementation(() => mockResponse(401, { error: 'Unauthorized' }));
        await expect(api.fetchMessages('ko')).rejects.toThrow('Unauthorized');
    });

    it('should handle non-JSON error responses', async () => {
        fetch.mockImplementation(() => mockResponse(500, 'Server Error', 'text/plain'));
        await expect(api.fetchMessages('ko')).rejects.toThrow('Fetch messages failed');
    });

    it('should send correct body for toggleDone', async () => {
        fetch.mockImplementation(() => mockResponse(200, { success: true }));
        await api.toggleDone(123, true);
        expect(fetch).toHaveBeenCalledWith('/api/messages/done', expect.objectContaining({
            method: 'POST',
            body: JSON.stringify({ id: 123, done: true })
        }));
    });

    it('should fetch archive with multiple query params', async () => {
        fetch.mockImplementation(() => mockResponse(200, { total: 0, messages: [] }));
        await api.fetchArchive({ q: 'test', limit: 10, offset: 20, lang: 'en', sort: 'time', order: 'ASC' });
        
        const callUrl = fetch.mock.calls[0][0];
        expect(callUrl).toContain('q=test');
        expect(callUrl).toContain('limit=10');
        expect(callUrl).toContain('offset=20');
        expect(callUrl).toContain('lang=en');
        expect(callUrl).toContain('sort=time');
        expect(callUrl).toContain('order=ASC');
    });

    it('should handle network fetch rejection (TypeError)', async () => {
        fetch.mockImplementation(() => Promise.reject(new TypeError('Failed to fetch')));
        await expect(api.fetchMessages('ko')).rejects.toThrow('Failed to fetch');
    });

    it('should parse text/html as Session Expired Error', async () => {
        // Mock a 200 OK but with text/html content type (login redirect)
        fetch.mockImplementation(() => mockResponse(200, '<html>login</html>', 'text/html'));
        await expect(api.fetchMessages('ko')).rejects.toThrow('Authentication Required (Session Expired or Proxy Redirect)');
    });
});
