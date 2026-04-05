import { expect, test, describe } from 'vitest';
import { normalizeReportData, getReportSummary } from './logic';
import { upsertReport, state } from './state';

describe('Report Logic & Caching', () => {
    test('normalizeReportData should handle string and object visualization_data', () => {
        const rawJson = JSON.stringify({ nodes: [{ id: '1', name: 'Test', value: 10 }], links: [] });
        const data = {
            id: '123',
            visualization_data: rawJson,
            report_summary: 'Hello',
            user_email: 'test@example.com',
            start_date: '2024-01-01',
            end_date: '2024-01-07'
        };
        const normalized = normalizeReportData(data);
        expect(normalized.id).toBe(123);
        // @ts-ignore
        expect(normalized.visualization_data.nodes[0].name).toBe('Test');
    });

    test('normalizeReportData should fallback on invalid JSON', () => {
        const data = { visualization_data: 'invalid-json' };
        const normalized = normalizeReportData(data);
        expect(normalized.visualization_data).toEqual({ nodes: [], links: [] });
    });

    test('getReportSummary should return translation if available', () => {
        const report = {
            report_summary: 'English',
            translations: { ko: '한국어' }
        } as any;
        expect(getReportSummary(report, 'ko')).toBe('한국어');
        expect(getReportSummary(report, 'en')).toBe('English');
    });

    test('upsertReport should store and retrieve by date key', () => {
        const report = {
            id: 1,
            user_email: 'test@example.com',
            start_date: '2024-01-01',
            end_date: '2024-01-07',
            report_summary: 'Weekly',
            visualization_data: { nodes: [], links: [] }
        };
        upsertReport(report);
        const key = '2024-01-01_2024-01-07';
        expect(state.reports[key]).toBeDefined();
        expect(state.reports[key].report_summary).toBe('Weekly');
    });
});
