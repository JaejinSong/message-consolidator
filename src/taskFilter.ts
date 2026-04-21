import { Message } from './types';
export type TaskTab = 'received' | 'delegated' | 'reference' | 'all';
/**
 * @file taskFilter.ts
 * @description Backend-driven filtering logic. Compressed < 30 lines.
 */
export function filterByTab(messages: Message[], tab: TaskTab): Message[] {
    if (!messages || tab === 'all') return messages || [];
    return messages.filter(m => {
        const cat = m.category || 'others';
        if (tab === 'received') return cat === 'personal';
        if (tab === 'delegated') return cat === 'requested';
        if (tab === 'reference') return ['shared', 'others'].includes(cat);
        return true;
    });
}
