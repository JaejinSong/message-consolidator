import { describe, it, expect, vi, afterEach } from 'vitest';
vi.unmock('marked');
import {
    // Data Logic
    calculateHeatmapLevel,
    processTimeSeriesData,
    // Filter Logic
    sortAndSearchMessages,
    getArchiveThresholdDays,
    // Format Logic
    getDeadlineBadge,
    parseMarkdown
} from './logic.ts';

// ----------------------------------------------------------------------
// 1. Data Processing Logic
// ----------------------------------------------------------------------
describe('logic.js - Data Processing', () => {
    describe('calculateHeatmapLevel', () => {
        it('should return correct level based on task count', () => {
            expect(calculateHeatmapLevel(0)).toBe(0);
            expect(calculateHeatmapLevel(2)).toBe(1);
            expect(calculateHeatmapLevel(4)).toBe(2);
            expect(calculateHeatmapLevel(6)).toBe(3);
            expect(calculateHeatmapLevel(10)).toBe(4);
            expect(calculateHeatmapLevel(-5)).toBe(0); // Edge case: negative
        });
    });


    describe('processTimeSeriesData', () => {
        it('should generate continuous daily data and handle missing days', () => {
            const today = new Date();
            today.setHours(0, 0, 0, 0);
            const yesterday = new Date(today);
            yesterday.setDate(yesterday.getDate() - 1);
            const yStr = yesterday.toISOString().split('T')[0];

            const rawHistory = [
                { date: yStr, counts: { slack: 5, telegram: 2 } }
            ];

            const processed = processTimeSeriesData(rawHistory, 3);
            expect(processed.length).toBe(3);
            expect(processed[1].cumulative).toBe(7);
        });

        it('should return empty array if days is 0 or negative', () => {
            expect(processTimeSeriesData([], 0).length).toBe(0);
            expect(processTimeSeriesData([], -5).length).toBe(0);
        });
    });
});

// ----------------------------------------------------------------------
// 2. Filtering & Classification Logic
// ----------------------------------------------------------------------
describe('logic.js - Filtering & Classification', () => {
    const mockMessages = [
        { id: 1, requester: 'Alice', task: 'Hello', source: 'slack', timestamp: '2023-01-01T12:00:00Z', done: false, assignee: 'me' },
        { id: 2, requester: 'Bob', task: 'World', source: 'whatsapp', timestamp: '2023-01-01T10:00:00Z', done: false, assignee: 'other' },
        { id: 3, requester: 'Charlie', task: 'Wait', source: 'slack', timestamp: '2023-01-01T11:00:00Z', done: false, waiting_on: 'Dave' },
    ];

    describe('sortAndSearchMessages', () => {
        it('should correctly filter tasks by search query', () => {
            const messages = [
                { id: 1, task: 'Buy milk', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
                { id: 2, task: 'Clean room', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
            ];
            
            expect(sortAndSearchMessages(messages, 'mIlK').length).toBe(1);
            expect(sortAndSearchMessages(messages, 'BOB').length).toBe(1);
        });

        it('should handle special characters in search query safely', () => {
            const messages = [
                { id: 1, task: 'Fix $ bug', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
                { id: 2, task: 'Normal task', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
            ];
            expect(sortAndSearchMessages(messages, '$').length).toBe(1);
        });
    });

});

// ----------------------------------------------------------------------
// 3. Formatting Logic
// ----------------------------------------------------------------------
describe('logic.js - Formatting', () => {
    describe('getDeadlineBadge', () => {
        afterEach(() => {
            vi.useRealTimers();
        });

        it('should return empty string if task is done or date is missing', () => {
            expect(getDeadlineBadge(new Date().toISOString(), true)).toBe('');
            expect(getDeadlineBadge(null, false)).toBe('');
        });

        it('should return stale badge after 24 hours of inactivity', () => {
            const now = new Date('2026-03-25T12:00:00Z');
            vi.useFakeTimers();
            vi.setSystemTime(now);

            const staleDate = new Date(now.getTime() - 26 * 60 * 60 * 1000).toISOString();
            const badge = getDeadlineBadge(staleDate, false, 'ko');
            expect(badge).toContain('c-badge--priority-medium');
            expect(badge).toContain('정체됨');
        });

        it('should return abandoned badge after 72 hours (excluding weekends)', () => {
            const past = new Date('2026-03-12T10:00:00Z').toISOString();
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-03-18T10:00:00Z'));
            const badge = getDeadlineBadge(past, false, 'ko');
            expect(badge).toContain('c-badge--priority-high');
            expect(badge).toContain('방치됨');
        });

        it('should strictly ignore weekends in duration calculation', () => {
            const fri = '2026-03-20T12:00:00Z';
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-03-23T13:00:00Z')); // ~73 hours physical time later
            const badge = getDeadlineBadge(fri, false, 'ko');
            // 73 - 48 (weekend) = 25 business hours -> Stale
            expect(badge).toContain('c-badge--priority-medium');
            expect(badge).not.toContain('c-badge--priority-high');
        });
    });

    describe('parseMarkdown', () => {
        it('should parse headers correctly', () => {
            expect(parseMarkdown('# Hello')).toContain('<h1');
            expect(parseMarkdown('## World')).toContain('<h2');
            expect(parseMarkdown('### Sub')).toContain('<h3');
        });

        it('should parse bold, italic, and code markdown elements', () => {
            expect(parseMarkdown('**bold**')).toContain('<strong');
            expect(parseMarkdown('`code`')).toContain('<code');
        });

        it('should parse unordered lists', () => {
            const html = parseMarkdown('- item');
            expect(html).toContain('<ul');
            expect(html).toContain('<li');
            expect(html).toContain('item');
        });
        
        it('should gracefully handle empty or null input', () => {
            expect(parseMarkdown('')).toBe('');
            expect(parseMarkdown(null)).toBe('');
        });
    });
});
