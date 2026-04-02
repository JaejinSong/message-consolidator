/**
 * verify-dashboard.ts
 * Type-safe logic verification using tsx.
 * Why: Logic-First Verification with high fidelity as requested.
 */

// Mock localStorage for Node environment
(global as any).localStorage = {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
    clear: () => {}
};

import { MessageCard, MessageCardProps } from './src/components/message-card.ts';

// Mocking required for pure component if any imports were tricky.
// Since we are running with tsx, we can try to let the real imports work 
// or provide a mock for things that might fail (like locales.js if it's not and-supported).

const testProps: MessageCardProps = {
    id: 123,
    source: 'slack',
    room: 'General',
    task: 'Fix the UI data rendering issue',
    requester: 'Jaejin Song',
    assignee: 'Antigravity',
    timestamp: '2026-04-02T01:00:00Z',
    done: false,
    category: 'TASK',
    metadata: null,
    lang: 'ko',
    has_original: true
};

try {
    const html = MessageCard(testProps);
    
    const checks = {
        hasRequester: html.includes('Jaejin Song') && html.includes('c-message-card__requester'),
        hasAssignee: html.includes('Antigravity') && html.includes('c-message-card__assignee'),
        hasTime: html.includes('c-message-card__timestamp'), // Should have formatted time
        hasActions: html.includes('data-action="delete"') && html.includes('data-action="toggle-done"'),
        hasBEM: html.includes('c-message-card__info-group') && html.includes('c-message-card__time-group')
    };

    console.log("Check results:", checks);

    if (!checks.hasRequester) throw new Error("Requester check failed");
    if (!checks.hasAssignee) throw new Error("Assignee check failed");
    if (!checks.hasTime) throw new Error("Time check failed (Check Data Mapping in renderer.ts)");
    if (!checks.hasActions) throw new Error("Actions check failed");
    if (!checks.hasBEM) throw new Error("BEM structure check failed");

    console.log("\nSUCCESS: Dashboard rendering logic verified via tsx.");
} catch (e) {
    console.error("FAIL: Verification failed", e);
    process.exit(1);
}
