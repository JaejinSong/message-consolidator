import { Message } from './types';
export type TaskTab = 'received' | 'delegated' | 'reference' | 'all';
export type DeadlineFilter = 'all' | 'today' | 'week' | 'has_deadline';

export function filterByTab(messages: Message[], tab: TaskTab): Message[] {
    if (!messages || tab === 'all' || tab === 'received') return messages || [];
    return messages.filter(m => {
        const cat = m.category || 'others';
        if (tab === 'delegated') return cat === 'requested';
        if (tab === 'reference') return cat !== 'requested';
        return true;
    });
}

export function filterByDeadline(messages: Message[], filter: DeadlineFilter): Message[] {
    if (!messages || filter === 'all') return messages || [];
    const todayStr = new Date().toISOString().slice(0, 10);
    const today = new Date(todayStr);
    return messages.filter(m => {
        const dl = m.deadline;
        if (!dl) return false;
        const d = new Date(dl.slice(0, 10));
        if (isNaN(d.getTime())) return false;
        const diffDays = Math.round((d.getTime() - today.getTime()) / 86400000);
        if (filter === 'today') return diffDays === 0;
        if (filter === 'week') return diffDays >= -1 && diffDays <= 7;
        if (filter === 'has_deadline') return true;
        return true;
    });
}
