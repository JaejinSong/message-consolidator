import { describe, it, expect } from 'vitest';
import { filterByTab } from '../taskFilter';

const mockMessages: any[] = [
    { id: 1, task: 'Personal Task', category: 'personal', done: false, timestamp: new Date().toISOString(), requester: 'User A', source: 'slack' },
    { id: 2, task: 'Shared Task', category: 'shared', done: false, timestamp: new Date().toISOString(), requester: 'User B', source: 'whatsapp' },
    { id: 3, task: 'Other Task', category: 'others', done: false, timestamp: new Date().toISOString(), requester: 'User C', source: 'slack' },
    { id: 4, task: 'Requested Task', category: 'requested', done: false, timestamp: new Date().toISOString(), requester: 'User D', source: 'slack' },
    { id: 5, task: 'Done Personal', category: 'personal', done: true, timestamp: new Date().toISOString(), requester: 'User A', source: 'slack' }
];

describe('taskFilter.ts - filterByTab', () => {
    it('should return all messages for "received" tab (inbox is pre-filtered server-side)', () => {
        const result = filterByTab(mockMessages, 'received');
        expect(result.length).toBe(mockMessages.length);
    });

    it('should filter messages for "delegated" tab (requested only)', () => {
        const result = filterByTab(mockMessages, 'delegated');
        expect(result.length).toBe(1);
        expect(result[0].id).toBe(4);
    });

    it('should filter messages for "reference" tab (everything except requested)', () => {
        const result = filterByTab(mockMessages, 'reference');
        expect(result.length).toBe(4); // personal×2, shared, others — excludes only 'requested'
        const ids = result.map(m => m.id);
        expect(ids).toContain(1);
        expect(ids).toContain(2);
        expect(ids).toContain(3);
        expect(ids).not.toContain(4); // requested → delegated tab only
    });

    it('should show personal-category items in reference when they appear in pending (divergence case)', () => {
        const pendingWithPersonal: any[] = [
            { id: 20, task: 'Name-matched task', category: 'personal', done: false, requester: 'User X', source: 'slack' },
            { id: 21, task: 'Shared task', category: 'shared', done: false, requester: 'User Y', source: 'slack' },
        ];
        const result = filterByTab(pendingWithPersonal, 'reference');
        expect(result.length).toBe(2);
        expect(result.map(m => m.id)).toContain(20);
    });

    it('should return all tasks in "all" tab', () => {
        const result = filterByTab(mockMessages, 'all');
        expect(result.length).toBe(5);
    });

    it('should return all for "all" and "received" tabs regardless of category', () => {
        const msgs: any[] = [
            { id: 10, task: 'Task A', done: false, requester: 'User A', source: 'slack' },
            { id: 11, task: 'Task B', done: false, requester: 'User B', source: 'slack' }
        ];
        expect(filterByTab(msgs, 'received').length).toBe(2);
        expect(filterByTab(msgs, 'all').length).toBe(2);
    });
});
