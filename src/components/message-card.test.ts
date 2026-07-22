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

describe('MessageCard exclusion candidate banner', () => {
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

    it('renders the banner when exclusion_candidate is pending', () => {
        const html = MessageCard({
            ...baseMessage,
            metadata: { exclusion_candidate: { status: 'pending', days_stalled: 35 } }
        } as any);

        expect(html).toContain('c-message-card__completion-candidate--exclusion');
        expect(html).toContain('data-action="confirm-exclusion"');
        expect(html).toContain('data-action="dismiss-exclusion"');
        expect(html).toContain('35일 이상 멈춰있는 업무입니다');
    });

    it('defaults to 31 days when days_stalled is absent', () => {
        const html = MessageCard({
            ...baseMessage,
            metadata: { exclusion_candidate: { status: 'pending' } }
        } as any);

        expect(html).toContain('31일 이상 멈춰있는 업무입니다');
    });

    it('does not render when the task is done', () => {
        const html = MessageCard({
            ...baseMessage,
            done: true,
            metadata: { exclusion_candidate: { status: 'pending' } }
        } as any);

        expect(html).not.toContain('c-message-card__completion-candidate--exclusion');
    });

    it('does not render when candidate status is not pending', () => {
        const html = MessageCard({
            ...baseMessage,
            metadata: { exclusion_candidate: { status: 'confirmed' } }
        } as any);

        expect(html).not.toContain('c-message-card__completion-candidate--exclusion');
    });

    it('yields to a pending completion candidate (banners never stack)', () => {
        const html = MessageCard({
            ...baseMessage,
            metadata: {
                completion_candidate: { status: 'pending' },
                exclusion_candidate: { status: 'pending' }
            }
        } as any);

        expect(html).toContain('data-action="confirm-candidate"');
        expect(html).not.toContain('c-message-card__completion-candidate--exclusion');
    });

    it('supports English copy', () => {
        const html = MessageCard({
            ...baseMessage,
            lang: 'en',
            metadata: { exclusion_candidate: { status: 'pending', days_stalled: 40 } }
        } as any);

        expect(html).toContain('Idle for 40+ days');
        expect(html).toContain('exclude from tracking?');
    });
});
