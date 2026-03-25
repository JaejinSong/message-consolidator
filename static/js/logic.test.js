import { describe, it, expect } from 'vitest';
import {
    sortAndFilterMessages,
    classifyMessages,
    calculateHeatmapLevel,
    calculateSourceDistribution,
    processTimeSeriesData,
    getDeadlineBadge,
    parseMarkdown,
    getArchiveThresholdDays
} from './logic.js';

const mockMessages = [
    { id: 1, requester: 'Alice', task: 'Hello', source: 'slack', timestamp: '2023-01-01T12:00:00Z', done: false, assignee: 'me' },
    { id: 2, requester: 'Bob', task: 'World', source: 'whatsapp', timestamp: '2023-01-01T10:00:00Z', done: false, assignee: 'other' },
    { id: 3, requester: 'Charlie', task: 'Wait', source: 'slack', timestamp: '2023-01-01T11:00:00Z', done: false, waiting_on: 'Dave' },
];

describe('logic.js - sortAndFilterMessages', () => {
    it('should correctly filter tasks for each tab', () => {
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
});

describe('logic.js - classifyMessages', () => {
    it('should correctly count tasks by category', () => {
        const complexMock = [
            { id: 1, done: false, assignee: 'me' },
            { id: 2, done: false, assignee: 'other' },
            { id: 3, done: false, assignee: 'me', waiting_on: 'Someone' },
            { id: 4, done: false, assignee: 'other', waiting_on: 'Someone' },
            { id: 5, done: true, assignee: 'me' }
        ];

        const counts = classifyMessages(complexMock);
        expect(counts.all).toBe(4);
        expect(counts.my).toBe(1);
        expect(counts.others).toBe(1);
        expect(counts.waiting).toBe(2);
    });

    it('should ignore completed tasks', () => {
        const onlyDoneMock = [
            { id: 10, done: true, assignee: 'me' },
            { id: 11, done: true, waiting_on: 'boss' }
        ];
        const emptyCounts = classifyMessages(onlyDoneMock);
        expect(emptyCounts.all).toBe(0);
        expect(emptyCounts.my).toBe(0);
        expect(emptyCounts.waiting).toBe(0);
    });
});

describe('logic.js - calculateHeatmapLevel', () => {
    it('should return correct level based on task count', () => {
        expect(calculateHeatmapLevel(0)).toBe(0);
        expect(calculateHeatmapLevel(2)).toBe(1);
        expect(calculateHeatmapLevel(4)).toBe(2);
        expect(calculateHeatmapLevel(6)).toBe(3);
        expect(calculateHeatmapLevel(10)).toBe(4);
        expect(calculateHeatmapLevel(-5)).toBe(0);
    });
});

describe('logic.js - calculateSourceDistribution', () => {
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
});

describe('logic.js - getDeadlineBadge', () => {
    it('should return empty string if task is done', () => {
        expect(getDeadlineBadge(new Date().toISOString(), true)).toBe('');
    });

    it('should return stale badge after 24 hours', () => {
        const staleDate = new Date(Date.now() - 25 * 60 * 60 * 1000).toISOString();
        const badge = getDeadlineBadge(staleDate, false, 'ko');
        expect(badge).toContain('badge-stale');
        expect(badge).toContain('정체됨');
    });

    it('should return abandoned badge after 72 hours (excluding weekends)', () => {
        // Thursday to Wednesday next week (enough time even with weekends)
        const past = new Date('2026-03-12T10:00:00Z').toISOString();
        vi.setSystemTime(new Date('2026-03-18T10:00:00Z')); // 6 days later (including Sat/Sun)
        const badge = getDeadlineBadge(past, false, 'ko');
        expect(badge).toContain('badge-abandoned');
        expect(badge).toContain('방치됨');
        vi.useRealTimers();
    });

    it('should ignore weekends in calculation', () => {
        // Friday to Monday 
        const fri = '2026-03-20T12:00:00Z';
        vi.setSystemTime(new Date('2026-03-23T13:00:00Z')); // ~73 hours elapsed
        const badge = getDeadlineBadge(fri, false, 'ko');
        // 73 hours - 48 hours (weekend) = 25 hours -> Stale
        expect(badge).toContain('badge-stale');
        expect(badge).not.toContain('badge-abandoned');
        vi.useRealTimers();
    });
});

describe('logic.js - parseMarkdown', () => {
    it('should parse headers correctly', () => {
        expect(parseMarkdown('# Hello')).toContain('<h1');
        expect(parseMarkdown('## World')).toContain('<h2');
        expect(parseMarkdown('### Sub')).toContain('<h3');
    });

    it('should parse bold and code', () => {
        expect(parseMarkdown('**bold**')).toContain('<strong');
        expect(parseMarkdown('**bold**')).toContain('bold</strong>');
        expect(parseMarkdown('`code`')).toContain('<code');
        expect(parseMarkdown('`code`')).toContain('code</code>');
    });

    it('should parse lists', () => {
        expect(parseMarkdown('- item')).toContain('•');
        expect(parseMarkdown('- item')).toContain('item</div>');
    });
});

describe('logic.js - sortAndFilterMessages (Search)', () => {
    it('should filter by search query (task or requester)', () => {
        const messages = [
            { id: 1, task: 'Buy milk', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
            { id: 2, task: 'Clean room', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        
        const filteredByTask = sortAndFilterMessages(messages, 'allTasksTab', 'milk');
        expect(filteredByTask.length).toBe(1);
        expect(filteredByTask[0].id).toBe(1);

        const filteredByRequester = sortAndFilterMessages(messages, 'allTasksTab', 'bob');
        expect(filteredByRequester.length).toBe(1);
        expect(filteredByRequester[0].id).toBe(2);
    });

    it('should handle special characters in search query', () => {
        const messages = [
            { id: 1, task: 'Fix $ bug', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
            { id: 2, task: 'Normal task', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        const filtered = sortAndFilterMessages(messages, 'allTasksTab', '$');
        expect(filtered.length).toBe(1);
        expect(filtered[0].id).toBe(1);
    });

    it('should handle null or undefined fields graceully during filtering', () => {
        const brokenMessages = [
            { id: 1, task: null, requester: undefined, source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        // Should not throw and should handle as empty strings
        const filtered = sortAndFilterMessages(brokenMessages, 'allTasksTab', 'anything');
        expect(filtered.length).toBe(0);
        
        const all = sortAndFilterMessages(brokenMessages, 'allTasksTab', '');
        expect(all.length).toBe(1);
    });
});

describe('logic.js - processTimeSeriesData', () => {
    it('should generate continuous daily data', () => {
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
});
