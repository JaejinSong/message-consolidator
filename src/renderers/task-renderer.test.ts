import { describe, it, expect } from 'vitest';
import { createCardElement } from './task-renderer.ts';

describe('task-renderer component', () => {
    it('should generate correct HTML with updated action classes', () => {
        const mockMsg = {
            id: 'slack-12345',
            task: 'Test Task',
            source: 'slack',
            requester: 'User A',
            assignee: 'me',
            done: false,
            timestamp: new Date().toISOString(),
            has_original: true
        };

        const html = createCardElement(mockMsg);
        
        // Check for new classes
        expect(html).toContain('view-original-btn');
        expect(html).toContain('delete-btn');
        expect(html).toContain('toggle-done-btn');
        
        // Check for ID preservation
        expect(html).toContain('data-id="slack-12345"');
    });

    it('should show original button only if has_original is true', () => {
        const mockMsg = {
            id: 'whatsapp-67890',
            task: 'Test Task',
            source: 'whatsapp',
            requester: 'User B',
            assignee: 'other',
            done: false,
            timestamp: new Date().toISOString(),
            has_original: false
        };

        const html = createCardElement(mockMsg);
        expect(html).not.toContain('view-original-btn');
    });
});
