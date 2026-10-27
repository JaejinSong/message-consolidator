import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: Error) => void;
    const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
    return { promise, resolve, reject };
}

function response(text: string): Response {
    return new Response(JSON.stringify({ results: [{ id: 20, translated_text: text }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
    });
}

describe('translation source title identity', () => {
    beforeEach(() => { vi.useFakeTimers(); vi.resetModules(); vi.mocked(fetch).mockReset(); });
    afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

    it.each(['old-first', 'new-first'])('separates in-flight revisions when responses arrive %s', async order => {
        const { api } = await import('./api');
        const old = deferred<Response>();
        const latest = deferred<Response>();
        vi.mocked(fetch).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
        const oldResult = api.requestTranslation(20, 'ko', 'Old title');
        await vi.advanceTimersByTimeAsync(50);
        const latestResult = api.requestTranslation(20, 'ko', 'New title');
        await vi.advanceTimersByTimeAsync(50);
        const calls = vi.mocked(fetch).mock.calls.map(([, options]) => JSON.parse(String(options?.body)));
        if (order === 'new-first') {
            latest.resolve(response('New translation'));
            await vi.advanceTimersByTimeAsync(0);
            old.resolve(response('Old translation'));
        } else {
            old.resolve(response('Old translation'));
            await vi.advanceTimersByTimeAsync(0);
            latest.resolve(response('New translation'));
        }
        expect(await oldResult).toBe('Old translation');
        expect(await latestResult).toBe('New translation');
        expect(calls).toEqual([
            { task_ids: [20], lang: 'ko', source_titles: { 20: 'Old title' } },
            { task_ids: [20], lang: 'ko', source_titles: { 20: 'New title' } },
        ]);
    });

    it('keeps two queued revisions in distinct payloads while deduplicating an identical revision', async () => {
        const { api } = await import('./api');
        vi.mocked(fetch).mockImplementation(async (_url, options) => {
            const body = JSON.parse(String(options?.body));
            return response(body.source_titles?.['20'] ?? 'missing source');
        });
        const old = api.requestTranslation(20, 'ko', 'Old title');
        const latest = api.requestTranslation(20, 'ko', 'New title');
        const duplicate = api.requestTranslation(20, 'ko', 'New title');
        await vi.advanceTimersByTimeAsync(50);
        expect(await Promise.all([old, latest, duplicate])).toEqual(['Old title', 'New title', 'New title']);
        expect(fetch).toHaveBeenCalledTimes(2);
    });

    it('cleans up an old failure without rejecting or removing the current revision', async () => {
        const { api } = await import('./api');
        const old = deferred<Response>();
        const latest = deferred<Response>();
        vi.mocked(fetch).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
        const oldResult = api.requestTranslation(20, 'ko', 'Old title').catch((error: unknown) => error);
        await vi.advanceTimersByTimeAsync(50);
        const latestResult = api.requestTranslation(20, 'ko', 'New title').catch((error: unknown) => error);
        await vi.advanceTimersByTimeAsync(50);
        old.reject(new Error('Old request failed'));
        await vi.advanceTimersByTimeAsync(0);
        const duplicate = api.requestTranslation(20, 'ko', 'New title').catch((error: unknown) => error);
        latest.resolve(response('New translation'));
        await vi.advanceTimersByTimeAsync(50);
        expect(await oldResult).toBeInstanceOf(Error);
        expect(await latestResult).toBe('New translation');
        expect(await duplicate).toBe('New translation');
        expect(fetch).toHaveBeenCalledTimes(2);
    });
});
