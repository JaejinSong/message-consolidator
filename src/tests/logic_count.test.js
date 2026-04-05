import { describe, it, expect } from 'vitest';
import { getActiveCount } from '../logic';

describe('logic.js - getActiveCount', () => {
    it('should correctly count only active messages (not done, not deleted)', () => {
        const messages = [
            { id: 1, done: false, is_deleted: false }, // Active
            { id: 2, done: true, is_deleted: false },  // Resolved (Exclude)
            { id: 3, done: false, is_deleted: true },  // Deleted (Exclude)
            { id: 4, done: false, is_deleted: 0 },     // Active (0 is falsy)
            { id: 5, done: false, is_deleted: 1 },     // Deleted (1 is truthy)
            { id: 6, done: false },                    // Active (undefined is falsy)
            { id: 7, done: true, is_deleted: true },   // Both (Exclude)
        ];
        
        // Expected: id 1, 4, 6 are active.
        expect(getActiveCount(messages)).toBe(3);
    });

    it('should return 0 for empty, null, or undefined lists', () => {
        expect(getActiveCount([])).toBe(0);
        expect(getActiveCount(undefined)).toBe(0);
        expect(getActiveCount(null)).toBe(0);
    });

    it('should handle missing done or is_deleted fields gracefully', () => {
        const messages = [
            { id: 1 }, // Active (both undefined)
            { id: 2, done: false }, // Active
            { id: 3, is_deleted: false }, // Active
        ];
        expect(getActiveCount(messages)).toBe(3);
    });
});
