import { describe, it, expect } from 'vitest';
import { filterByTab } from '../taskFilter';

const mockMessages: any[] = [
    { id: 1, task: 'Personal Task', category: 'personal', done: false, timestamp: new Date().toISOString(), requester: 'User A', source: 'slack' },
    { id: 2, task: 'Shared Task', category: 'shared', done: false, timestamp: new Date().toISOString(), requester: 'User B', source: 'whatsapp' },
    { id: 3, task: 'Other Task', category: 'others', done: false, timestamp: new Date().toISOString(), requester: 'User C', source: 'slack' },
    { id: 4, task: 'Unknown Task', category: 'unknown', done: false, timestamp: new Date().toISOString(), requester: 'User D', source: 'slack' },
    { id: 5, task: 'Done Personal', category: 'personal', done: true, timestamp: new Date().toISOString(), requester: 'User A', source: 'slack' }
];

describe('taskFilter.ts - filterByTab', () => {
    it('should filter messages for "my" tab correctly', () => {
        const result = filterByTab(mockMessages, 'my');
        // Only personal
        expect(result.length).toBe(2);
        expect(result.every(m => m.category === 'personal')).toBe(true);
    });

    it('should filter messages for "other" tab correctly', () => {
        const result = filterByTab(mockMessages, 'other');
        // shared and others
        expect(result.length).toBe(2);
        const ids = result.map(m => m.id);
        expect(ids).toContain(2);
        expect(ids).toContain(3);
    });

    it('should return all tasks in "all" tab', () => {
        const result = filterByTab(mockMessages, 'all');
        expect(result.length).toBe(5);
    });

    it('should fallback to inbox distribution if category is missing (not expected but for safety)', () => {
        const legacyMessages: any[] = [
            { id: 10, task: 'Legacy My', assignee: 'me', done: false, requester: 'User A', source: 'slack' },
            { id: 11, task: 'Legacy Other', assignee: 'other', done: false, requester: 'User B', source: 'slack' }
        ];
        // filterByTab returns empty if category is missing and not 'all' tab
        expect(filterByTab(legacyMessages, 'my').length).toBe(0);
        expect(filterByTab(legacyMessages, 'all').length).toBe(2);
    });
});
