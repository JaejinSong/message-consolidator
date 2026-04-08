import { Message } from './types';
export type TaskTab = 'my' | 'other' | 'all';
/**
 * @file taskFilter.ts
 * @description Backend-driven filtering logic. Compressed < 30 lines.
 */
export function filterByTab(messages: Message[], tab: TaskTab): Message[] {
    if (!messages || tab === 'all') return messages || [];
    return messages.filter(m => {
        const cat = m.category || 'others';
        if (tab === 'my') return cat === 'personal';
        if (tab === 'other') return ['shared', 'others', 'requested'].includes(cat);
        return true;
    });
}
