import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from './api';

// Why: TranslationBatcher uses a 50ms debounce timer; fake timers let tests control flushing.
describe('api — TranslationBatcher (requestTranslation)', () => {
    beforeEach(() => {
        (fetch as ReturnType<typeof vi.fn>).mockClear();
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
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

    it('batches multiple ids into a single translateTasksBatch call', async () => {
        (fetch as ReturnType<typeof vi.fn>).mockImplementation(() =>
            mockResponse(200, { results: [{ id: 1, translated_text: 'one' }, { id: 2, translated_text: 'two' }] })
        );

        const p1 = api.requestTranslation(1, 'en');
        const p2 = api.requestTranslation(2, 'en');

        // Advance past the 50ms debounce
        await vi.advanceTimersByTimeAsync(100);

        const [r1, r2] = await Promise.all([p1, p2]);
        expect(r1).toBe('one');
        expect(r2).toBe('two');
        // Only one batch call should have been made
        expect(fetch).toHaveBeenCalledTimes(1);
    });

    it('deduplicates repeated requests for the same id+lang', async () => {
        (fetch as ReturnType<typeof vi.fn>).mockImplementation(() =>
            mockResponse(200, { results: [{ id: 5, translated_text: 'hello' }] })
        );

        const p1 = api.requestTranslation(5, 'ko');
        const p2 = api.requestTranslation(5, 'ko');

        await vi.advanceTimersByTimeAsync(100);

        const [r1, r2] = await Promise.all([p1, p2]);
        expect(r1).toBe('hello');
        expect(r2).toBe('hello');
        // Both promises resolve to the same value; only one network call
        expect(fetch).toHaveBeenCalledTimes(1);
    });

    it('groups different langs into separate batch calls', async () => {
        (fetch as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
            if ((url as string).includes('translate-batch')) {
                return mockResponse(200, { results: [{ id: 3, translated_text: 'translated' }] });
            }
            return mockResponse(200, {});
        });

        const pEn = api.requestTranslation(3, 'en');
        const pKo = api.requestTranslation(3, 'ko');

        await vi.advanceTimersByTimeAsync(100);

        const [rEn, rKo] = await Promise.all([pEn, pKo]);
        expect(rEn).toBe('translated');
        expect(rKo).toBe('translated');
        // Two separate batch calls — one per language
        expect(fetch).toHaveBeenCalledTimes(2);
    });

    it('resolves with empty string when result not found in batch response', async () => {
        (fetch as ReturnType<typeof vi.fn>).mockImplementation(() =>
            mockResponse(200, { results: [] })
        );

        const p = api.requestTranslation(9, 'en');
        await vi.advanceTimersByTimeAsync(100);

        const result = await p;
        expect(result).toBe('');
    });

    it('rejects all pending promises when batch API call fails', async () => {
        (fetch as ReturnType<typeof vi.fn>).mockImplementation(() =>
            mockResponse(500, 'error', 'text/plain')
        );

        const p1 = api.requestTranslation(7, 'fr').catch((e: unknown) => e);
        const p2 = api.requestTranslation(8, 'fr').catch((e: unknown) => e);

        await vi.advanceTimersByTimeAsync(100);

        const [r1, r2] = await Promise.all([p1, p2]);
        expect(r1).toBeInstanceOf(Error);
        expect(r2).toBeInstanceOf(Error);
    });

    it('rejects on invalid id', async () => {
        await expect(api.requestTranslation('not-a-number', 'en')).rejects.toThrow('Invalid ID');
    });
});
