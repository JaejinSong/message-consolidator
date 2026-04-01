import { describe, it, expect, vi, afterEach } from 'vitest';
import {
    // Data Logic
    calculateHeatmapLevel,
    calculateSourceDistribution,
    processTimeSeriesData,
    // Filter Logic
    sortAndFilterMessages,
    classifyMessages,
    getArchiveThresholdDays,
    // Format Logic
    getDeadlineBadge,
    parseMarkdown
} from './logic.js';

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

    describe('calculateSourceDistribution', () => {
        it('should calculate percentages correctly', () => {
            const dist = calculateSourceDistribution({ slack: 10, whatsapp: 10, gmail: 20 });
            expect(dist.slack).toBe(25);
            expect(dist.whatsapp).toBe(25);
            expect(dist.gmail).toBe(50);
        });

        it('should handle zero total tasks', () => {
            const distZero = calculateSourceDistribution({ slack: 0, whatsapp: 0, gmail: 0 });
            expect(Object.keys(distZero).length).toBe(0);
        });

        it('should handle undefined source objects', () => {
            const distUndef = calculateSourceDistribution(undefined);
            expect(Object.keys(distUndef).length).toBe(0);
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

    describe('sortAndFilterMessages', () => {
        it('should correctly filter tasks for each tab based on rules', () => {
            const now = new Date();
            const threshold = getArchiveThresholdDays();
            const recentDate = new Date(now.getTime() - (threshold * 0.5) * 24 * 60 * 60 * 1000).toISOString();
            const oldDate = new Date(now.getTime() - (threshold + 1) * 24 * 60 * 60 * 1000).toISOString();

            const dynamicMock = [
                ...mockMessages,
                { id: 4, requester: 'User D', task: 'Recent Done task', source: 'slack', timestamp: recentDate, done: true, assignee: 'me' },
                { id: 5, requester: 'User E', task: 'Old Done task', source: 'slack', timestamp: oldDate, done: true, assignee: 'me' },
                { id: 6, requester: 'NoDate', task: 'No date task', source: 'slack', done: false, assignee: 'me' },
                { id: 7, requester: 'Waiting Me', task: 'Waiting for me', source: 'slack', done: false, assignee: 'me', waiting_on: 'Dave' }
            ];

            // My Tasks Tab
            const myTasks = sortAndFilterMessages(dynamicMock, 'myTasksTab', '');
            expect(myTasks.some(t => t.id === 1)).toBe(true);
            expect(myTasks.some(t => t.id === 7)).toBe(false);
            expect(myTasks.length).toBe(3);

            // Other Tasks Tab
            const otherTasks = sortAndFilterMessages(dynamicMock, 'otherTasksTab', '');
            expect(otherTasks.length).toBe(1);
            expect(otherTasks[0].id).toBe(2);

            // Waiting Tasks Tab
            const waitingTasks = sortAndFilterMessages(dynamicMock, 'waitingTasksTab', '');
            expect(waitingTasks.length).toBe(2);

            // All Tasks Tab
            const allTasks = sortAndFilterMessages(dynamicMock, 'allTasksTab', '');
            expect(allTasks.length).toBe(6);
        });

        it('should filter correctly by complex search queries (case-insensitive)', () => {
            const messages = [
                { id: 1, task: 'Buy milk', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
                { id: 2, task: 'Clean room', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
            ];
            
            expect(sortAndFilterMessages(messages, 'allTasksTab', 'mIlK').length).toBe(1);
            expect(sortAndFilterMessages(messages, 'allTasksTab', 'BOB').length).toBe(1);
        });

        it('should handle special characters in search query safely', () => {
            const messages = [
                { id: 1, task: 'Fix $ bug', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
                { id: 2, task: 'Normal task', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
            ];
            expect(sortAndFilterMessages(messages, 'allTasksTab', '$').length).toBe(1);
        });

        it('should handle null or undefined fields gracefully during filtering', () => {
            const brokenMessages = [
                { id: 1, task: null, requester: undefined, source: 'slack', done: false, timestamp: new Date().toISOString() },
                { id: 2, task: 'undefined', requester: 'null', source: 'gmail', done: false }
            ];
            expect(sortAndFilterMessages(brokenMessages, 'allTasksTab', 'anything').length).toBe(0);
            expect(sortAndFilterMessages(brokenMessages, 'allTasksTab', '').length).toBe(2);
        });
    });

    describe('classifyMessages', () => {
        it('should accurately categorize tasks and ignore completed ones', () => {
            const complexMock = [
                { id: 1, done: false, assignee: 'me' },
                { id: 2, done: false, assignee: 'other' },
                { id: 3, done: false, assignee: 'me', waiting_on: 'Someone' },
                { id: 4, done: false, assignee: 'other', waiting_on: 'Someone' },
                { id: 5, done: true, assignee: 'me' } // Should be ignored
            ];

            const counts = classifyMessages(complexMock);
            expect(counts.all).toBe(4);
            expect(counts.my).toBe(1);
            expect(counts.others).toBe(1);
            expect(counts.waiting).toBe(2);
        });

        it('should handle completely empty message arrays', () => {
            const emptyCounts = classifyMessages([]);
            expect(emptyCounts.all).toBe(0);
            expect(emptyCounts.my).toBe(0);
            expect(emptyCounts.others).toBe(0);
            expect(emptyCounts.waiting).toBe(0);
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
            expect(badge).toContain('badge-stale');
            expect(badge).toContain('정체됨');
        });

        it('should return abandoned badge after 72 hours (excluding weekends)', () => {
            const past = new Date('2026-03-12T10:00:00Z').toISOString();
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-03-18T10:00:00Z'));
            const badge = getDeadlineBadge(past, false, 'ko');
            expect(badge).toContain('badge-abandoned');
            expect(badge).toContain('방치됨');
        });

        it('should strictly ignore weekends in duration calculation', () => {
            const fri = '2026-03-20T12:00:00Z';
            vi.useFakeTimers();
            vi.setSystemTime(new Date('2026-03-23T13:00:00Z')); // ~73 hours physical time later
            const badge = getDeadlineBadge(fri, false, 'ko');
            // 73 - 48 (weekend) = 25 business hours -> Stale
            expect(badge).toContain('badge-stale');
            expect(badge).not.toContain('badge-abandoned');
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
            expect(parseMarkdown('- item')).toContain('•');
            expect(parseMarkdown('- item')).toContain('item</div>');
        });
        
        it('should gracefully handle empty or null input', () => {
            expect(parseMarkdown('')).toBe('');
            expect(parseMarkdown(null)).toBe('');
        });
    });
});
