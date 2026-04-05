import { describe, it, expect, vi } from 'vitest';
import { events, EVENTS } from './events';

describe('events.js - EventEmitter', () => {
    it('should subscribe and emit events', () => {
        const mockHandler = vi.fn();
        events.on(EVENTS.TASK_COMPLETED, mockHandler);
        
        const testData = { id: 123 };
        events.emit(EVENTS.TASK_COMPLETED, testData);
        
        expect(mockHandler).toHaveBeenCalledWith(testData);
    });

    it('should handle multiple listeners for one event', () => {
        const h1 = vi.fn();
        const h2 = vi.fn();
        events.on('multitest', h1);
        events.on('multitest', h2);
        
        events.emit('multitest', 'val');
        expect(h1).toHaveBeenCalledWith('val');
        expect(h2).toHaveBeenCalledWith('val');
    });

    it('should unsubscribe from events', () => {
        const h = vi.fn();
        events.on('offtest', h);
        events.off('offtest', h);
        
        events.emit('offtest', 'nothing');
        expect(h).not.toHaveBeenCalled();
    });

    it('should do nothing if emitting event with no listeners', () => {
        expect(() => events.emit('nonexistent', {})).not.toThrow();
    });
});
