import { Message } from './types';
export type TaskTab = 'received' | 'delegated' | 'reference' | 'all';
export function filterByTab(messages: Message[], tab: TaskTab): Message[] {
    if (!messages || tab === 'all' || tab === 'received') return messages || [];
    return messages.filter(m => {
        const cat = m.category || 'others';
        if (tab === 'delegated') return cat === 'requested';
        if (tab === 'reference') return cat !== 'requested';
        return true;
    });
}
