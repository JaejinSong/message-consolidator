import { describe, it, expect } from 'vitest';
import { filterByDeadline } from '../taskFilter';
import { Message } from '../types';

const today = new Date().toISOString().slice(0, 10);
const nextWeek = new Date(Date.now() + 5 * 86400000).toISOString().slice(0, 10);
const overdue = new Date(Date.now() - 1 * 86400000).toISOString().slice(0, 10);

const mockMessages: Message[] = [
    { id: 1, task: 'Due today',    deadline: today,     done: false, requester: 'A', source: 'slack' } as Message,
    { id: 2, task: 'Due next week',deadline: nextWeek,  done: false, requester: 'B', source: 'slack' } as Message,
    { id: 3, task: 'Overdue',      deadline: overdue,   done: false, requester: 'C', source: 'slack' } as Message,
    { id: 4, task: 'No deadline',  deadline: undefined, done: false, requester: 'D', source: 'slack' } as Message,
];

describe('taskFilter.ts - filterByDeadline', () => {
    it('should return all messages for "all" filter', () => {
        expect(filterByDeadline(mockMessages, 'all').length).toBe(4);
    });

    it('should return only today tasks for "today" filter', () => {
        const result = filterByDeadline(mockMessages, 'today');
        expect(result.length).toBe(1);
        expect(result[0].id).toBe(1);
    });

    it('should return this-week tasks for "week" filter (includes overdue -1 to +7)', () => {
        const result = filterByDeadline(mockMessages, 'week');
        expect(result.map(m => m.id)).toContain(1);
        expect(result.map(m => m.id)).toContain(2);
        expect(result.map(m => m.id)).toContain(3);
        expect(result.map(m => m.id)).not.toContain(4);
    });

    it('should return only tasks with deadlines for "has_deadline" filter', () => {
        const result = filterByDeadline(mockMessages, 'has_deadline');
        expect(result.length).toBe(3);
        expect(result.map(m => m.id)).not.toContain(4);
    });
});
