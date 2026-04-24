import { describe, it, expect, beforeEach } from 'vitest';
import { state, deleteTaskFromState, updateTaskStatusInState, updateMessages } from './state';
import { Message } from './types';

describe('State Sync Logic', () => {
    beforeEach(() => {
        state.messages = {
            inbox: [
                { id: 1, task: 'Task 1', done: false } as Message,
                { id: 2, task: 'Task 2', done: false } as Message
            ],
            delegated: [
                { id: 3, task: 'Task 3', done: false } as Message
            ],
            reference: []
        };
    });

    it('should delete task and decrease length correctly', () => {
        deleteTaskFromState(1);
        expect(state.messages.inbox.length).toBe(1);
        expect(state.messages.inbox.find(m => m.id === 1)).toBeUndefined();
    });

    it('should handle deleting non-existent ID gracefully', () => {
        deleteTaskFromState(999);
        expect(state.messages.inbox.length).toBe(2);
        expect(state.messages.delegated.length).toBe(1);
    });

    it('should toggle status and maintain array length', () => {
        updateTaskStatusInState(1, true);
        const task = state.messages.inbox.find(m => m.id === 1);
        expect(task?.done).toBe(true);
        expect(task?.completed_at).toBeDefined();
        expect(state.messages.inbox.length).toBe(2);
    });

    it('should be robust against null messages in updateMessages', () => {
        // @ts-ignore
        updateMessages(null);
        expect(state.messages.inbox).toEqual([]);
        expect(state.messages.delegated).toEqual([]);
        expect(state.messages.reference).toEqual([]);
    });

    it('should handle malformed messages structure in updateMessages', () => {
        // @ts-ignore
        updateMessages({ something: 'else' });
        expect(state.messages.inbox).toEqual([]);
        expect(state.messages.delegated).toEqual([]);
        expect(state.messages.reference).toEqual([]);
    });
});
