import { describe, it, expect, vi, afterEach } from 'vitest';
vi.unmock('marked');
import {
    // Data Logic
    calculateHeatmapLevel,
    processTimeSeriesData,
    // Filter Logic
    sortAndSearchMessages,
    // Format Logic
    getDeadlineBadge,
    parseMarkdown,
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

        it('should return empty string if done or deadline is missing', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            expect(getDeadlineBadge('2026-04-23', true, 'ko')).toBe('');
            expect(getDeadlineBadge(undefined, false, 'ko')).toBe('');
            expect(getDeadlineBadge('', false, 'ko')).toBe('');
        });

        it('should return "오늘" badge when deadline is today', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-04-23', false, 'ko');
            expect(badge).toContain('c-badge--deadline-today');
            expect(badge).toContain('오늘');
        });

        it('should return "내일" badge when deadline is tomorrow', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-04-24', false, 'ko');
            expect(badge).toContain('c-badge--deadline-tomorrow');
            expect(badge).toContain('내일');
        });

        it('should return "D-N" badge for deadline 2-7 days away', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-04-27', false, 'ko');
            expect(badge).toContain('c-badge--deadline-soon');
            expect(badge).toContain('D-4');
        });

        it('should return "지남" badge for deadline 1-7 days past', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-04-20', false, 'ko');
            expect(badge).toContain('c-badge--deadline-past');
            expect(badge).toContain('지남');
        });

        it('should return empty string for deadline more than 7 days away', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-05-10', false, 'ko');
            expect(badge).toBe('');
        });

        it('should return empty string for deadline more than 7 days past', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            const badge = getDeadlineBadge('2026-04-01', false, 'ko');
            expect(badge).toBe('');
        });

        it('should use English labels for en locale', () => {
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
            expect(getDeadlineBadge('2026-04-23', false, 'en')).toContain('Today');
            expect(getDeadlineBadge('2026-04-24', false, 'en')).toContain('Tomorrow');
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
            expect(parseMarkdown(null as unknown as string)).toBe('');
        });
    });
});
