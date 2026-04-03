import { describe, it, expect } from 'vitest';
import { MessageCard } from './message-card';

describe('MessageCard Component', () => {
    const baseMessage = {
        id: 1,
        source: 'slack',
        room: 'General',
        task: 'Finish the report',
        requester: 'John Doe',
        assignee: 'me',
        timestamp: '2026-04-03T06:00:00Z',
        done: false,
        category: 'TASK',
        lang: 'ko'
    };

    it('should render delegated badge when assigned_to is present', () => {
        const props = {
            ...baseMessage,
            assigned_to: 'Jane Smith'
        };
        const html = MessageCard(props as any);
        
        expect(html).toContain('c-message-card__badge--delegated');
        expect(html).toContain('@Jane Smith에게 위임됨');
        expect(html).toContain('🔄');
    });

    it('should not render delegated badge when assigned_to is absent', () => {
        const html = MessageCard(baseMessage as any);
        
        expect(html).not.toContain('c-message-card__badge--delegated');
        expect(html).not.toContain('위임됨');
    });

    it('should support English translation for delegated badge', () => {
        const props = {
            ...baseMessage,
            lang: 'en',
            assigned_to: 'Jane Smith'
        };
        const html = MessageCard(props as any);
        
        expect(html).toContain('Delegated to @Jane Smith');
    });
});
