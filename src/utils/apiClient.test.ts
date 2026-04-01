import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiFetch } from './apiClient';

describe('apiFetch', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  it('should throw AuthError when status is 401', async () => {
    (fetch as any).mockResolvedValue({
      status: 401,
      ok: false,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => ({ error: 'unauthorized', code: 401 }),
    });

    try {
      await apiFetch('/test');
      expect.fail('Should have thrown an error');
    } catch (err: any) {
      expect(err.message).toBe('Session Expired or Unauthorized');
      expect(err.isAuthError).toBe(true);
      expect(err.status).toBe(401);
    }
  });

  it('should throw AuthError when response is HTML', async () => {
    (fetch as any).mockResolvedValue({
      status: 200,
      ok: true,
      headers: new Headers({ 'Content-Type': 'text/html' }),
      text: async () => '<html>Login Page</html>',
    });

    try {
      await apiFetch('/test');
      expect.fail('Should have thrown an error');
    } catch (err: any) {
      expect(err.message).toBe('Authentication Required (Unexpected HTML response)');
      expect(err.isAuthError).toBe(true);
    }
  });

  it('should return JSON when response is JSON', async () => {
    const mockData = { id: 1, text: 'hello' };
    (fetch as any).mockResolvedValue({
      status: 200,
      ok: true,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => mockData,
    });

    const result = await apiFetch('/test');
    expect(result).toEqual(mockData);
  });
});
