import gettingStarted from '../docs/user-guide/getting-started.md?raw';
import channels from '../docs/user-guide/channels.md?raw';
import tasks from '../docs/user-guide/tasks.md?raw';
import reports from '../docs/user-guide/reports.md?raw';
import faq from '../docs/user-guide/faq.md?raw';

export type GuideSection = 'getting-started' | 'channels' | 'tasks' | 'reports' | 'faq';

export const GUIDE_SECTIONS: { key: GuideSection; title: string; icon: string }[] = [
    { key: 'getting-started', title: 'Getting Started', icon: '🚀' },
    { key: 'channels',        title: 'Connecting Channels', icon: '🔗' },
    { key: 'tasks',           title: 'Managing Tasks', icon: '✅' },
    { key: 'reports',         title: 'Reports & Insights', icon: '📊' },
    { key: 'faq',             title: 'FAQ & Privacy', icon: '❓' },
];

export const GUIDE_CONTENT: Record<GuideSection, string> = {
    'getting-started': gettingStarted,
    'channels': channels,
    'tasks': tasks,
    'reports': reports,
    'faq': faq,
};
