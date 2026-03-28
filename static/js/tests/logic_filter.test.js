import { describe, it, expect } from 'vitest';
import {
    sortAndFilterMessages,
    classifyMessages,
    getArchiveThresholdDays
} from '../logic.js';

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
        const filtered = sortAndFilterMessages(brokenMessages, 'allTasksTab', 'anything');
        expect(filtered.length).toBe(0);
        
        const all = sortAndFilterMessages(brokenMessages, 'allTasksTab', '');
        expect(all.length).toBe(1);
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
