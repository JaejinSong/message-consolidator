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
    it('should filter messages for "received" tab (personal only)', () => {
        const result = filterByTab(mockMessages, 'received');
        expect(result.length).toBe(2);
        expect(result.every(m => m.category === 'personal')).toBe(true);
    });

    it('should filter messages for "delegated" tab (requested only)', () => {
        const result = filterByTab(mockMessages, 'delegated');
        expect(result.length).toBe(1);
        expect(result[0].id).toBe(4);
    });

    it('should filter messages for "reference" tab (shared + others)', () => {
        const result = filterByTab(mockMessages, 'reference');
        expect(result.length).toBe(2);
        const ids = result.map(m => m.id);
        expect(ids).toContain(2);
        expect(ids).toContain(3);
    });

    it('should return all tasks in "all" tab', () => {
        const result = filterByTab(mockMessages, 'all');
        expect(result.length).toBe(5);
    });

    it('should return empty for non-matching categories when not "all" tab', () => {
        const legacyMessages: any[] = [
            { id: 10, task: 'Legacy My', assignee: 'me', done: false, requester: 'User A', source: 'slack' },
            { id: 11, task: 'Legacy Other', assignee: 'other', done: false, requester: 'User B', source: 'slack' }
        ];
        expect(filterByTab(legacyMessages, 'received').length).toBe(0);
        expect(filterByTab(legacyMessages, 'all').length).toBe(2);
    });
});
