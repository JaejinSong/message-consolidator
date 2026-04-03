import { describe, it, expect } from 'vitest';
import {
    sortAndSearchMessages,
    getArchiveThresholdDays
} from '../logic.js';

const mockMessages = [
    { id: 1, requester: 'Alice', task: 'Hello', source: 'slack', timestamp: '2023-01-01T12:00:00Z', done: false, assignee: 'me' },
    { id: 2, requester: 'Bob', task: 'World', source: 'whatsapp', timestamp: '2023-01-01T10:00:00Z', done: false, assignee: 'other' },
    { id: 3, requester: 'Charlie', task: 'Wait', source: 'slack', timestamp: '2023-01-01T11:00:00Z', done: false, waiting_on: 'Dave' },
];

describe('logic.js - sortAndSearchMessages', () => {
    it('should correctly filter tasks by archive threshold and search query', () => {
        const now = new Date();
        const threshold = getArchiveThresholdDays();
        const recentDate = new Date(now.getTime() - (threshold * 0.5) * 24 * 60 * 60 * 1000).toISOString();
        const oldDate = new Date(now.getTime() - (threshold + 1) * 24 * 60 * 60 * 1000).toISOString();

        const dynamicMock = [
            ...mockMessages,
            { id: 4, requester: 'User D', task: 'Recent Done task', source: 'slack', timestamp: recentDate, done: true, assignee: 'me' },
            { id: 5, requester: 'User E', task: 'Old Done task', source: 'slack', timestamp: oldDate, done: true, assignee: 'me' }
        ];

        // Without search query
        const allAvailable = sortAndSearchMessages(dynamicMock, '');
        // id 1, 2, 3 (not done) + id 4 (done but recent) = 4 tasks. id 5 is old and done.
        expect(allAvailable.length).toBe(4);
        expect(allAvailable.some(t => t.id === 5)).toBe(false);

        // With search query
        const filtered = sortAndSearchMessages(dynamicMock, 'Alice');
        expect(filtered.length).toBe(1);
        expect(filtered[0].id).toBe(1);
    });

    it('should filter by search query (task or requester) case-insensitively', () => {
        const messages = [
            { id: 1, task: 'Buy milk', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
            { id: 2, task: 'Clean room', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        
        const filteredByTask = sortAndSearchMessages(messages, 'MILK');
        expect(filteredByTask.length).toBe(1);
        expect(filteredByTask[0].id).toBe(1);

        const filteredByRequester = sortAndSearchMessages(messages, 'BOB');
        expect(filteredByRequester.length).toBe(1);
        expect(filteredByRequester[0].id).toBe(2);
    });

    it('should handle special characters in search query safely', () => {
        const messages = [
            { id: 1, task: 'Fix $ bug', requester: 'Alice', source: 'slack', done: false, timestamp: new Date().toISOString() },
            { id: 2, task: 'Normal task', requester: 'Bob', source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        const filtered = sortAndSearchMessages(messages, '$');
        expect(filtered.length).toBe(1);
        expect(filtered[0].id).toBe(1);
    });

    it('should handle null or undefined fields gracefully during filtering', () => {
        const brokenMessages = [
            { id: 1, task: null, requester: undefined, source: 'slack', done: false, timestamp: new Date().toISOString() }
        ];
        const filtered = sortAndSearchMessages(brokenMessages, 'anything');
        expect(filtered.length).toBe(0);
        
        const all = sortAndSearchMessages(brokenMessages, '');
        expect(all.length).toBe(1);
    });
});

