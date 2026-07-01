// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import * as renderer from './renderer.ts';
import { I18N_DATA } from './locales';
import { state } from './state';
import type { Message } from './types';


describe('renderer.js - Empty State Messages', () => {
    it('should have a sufficient number of witty messages', () => {
        const lang = 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        expect(messages.length).toBeGreaterThanOrEqual(15);
        expect(messages.some((m: string) => m.includes('커피'))).toBe(true);
    });
});



describe('renderer.js - showToast', () => {
    it('should create and append a toast element', () => {
        renderer.showToast('Test Message', 'success');
        const toast = document.querySelector('.toast-popup') as HTMLElement;
        expect(toast).not.toBeNull();
        expect(toast.classList.contains('toast-success')).toBe(true);
        expect(toast.textContent).toContain('Test Message');
    });
});


describe('renderer.js - setScanLoading', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <button id="scanBtn"></button>
            <i id="scanBtnIcon"></i>
            <div id="loading" class="hidden"></div>
        `;
    });

    it('should toggle loading state', () => {
        renderer.setScanLoading(true);
        expect((document.getElementById('scanBtn') as HTMLButtonElement).disabled).toBe(true);
        expect((document.getElementById('loading') as HTMLElement).classList.contains('active')).toBe(true);

        renderer.setScanLoading(false);
        expect((document.getElementById('scanBtn') as HTMLButtonElement).disabled).toBe(false);
        expect((document.getElementById('loading') as HTMLElement).classList.contains('active')).toBe(false);
    });
});

describe('renderer.js - createCardElement', () => {
    it('should include promise tag when category is promise', () => {
        const msg = { id: 1, source: 'slack', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'promise', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--promise');
        expect(html).toContain('🤝');
    });

    it('should include shared tag when category is shared', () => {
        const msg = { id: 2, source: 'whatsapp', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'shared', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--shared');
        expect(html).toContain('👥');
    });

    it('should render legacy "me" assignee as a regular name', () => {
        const msg = { id: 3, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'me', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__assignee--other');
        expect(html).not.toContain('c-message-card__assignee--me');
    });

    it('should handle literal "undefined" or "unknown" assignee by not rendering it', () => {
        const msgUndef = { id: 4, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'undefined', room: 'R' };
        const htmlUndef = renderer.createCardElement(msgUndef);
        expect(htmlUndef).not.toContain('undefined');

        const msgUnknown = { id: 5, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'unknown', room: 'R' };
        const htmlUnknown = renderer.createCardElement(msgUnknown);
        expect(htmlUnknown).not.toContain('unknown');
    });

    it('should escape HTML in task, requester, and room to prevent XSS', () => {
        const xssMsg = {
            id: 6,
            source: 'slack',
            task: '<script>alert("xss")</script>',
            requester: '<b>Attacker</b>',
            room: '<img src=x onerror=alert(1)>',
            timestamp: new Date().toISOString(),
            done: false
        };
        const html = renderer.createCardElement(xssMsg);
        expect(html).toContain('&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;');
        expect(html).toContain('&lt;b&gt;Attacker&lt;/b&gt;');
        expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
        expect(html).not.toContain('<script>');
        expect(html).not.toContain('<b>');
    });
});

describe('renderer.js - updateUserProfile', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="userProfile" class="hidden"></div>
            <div id="userEmail" class="hidden"></div>
            <img id="userPicture" src="" class="hidden">
        `;
    });

    it('should unhide userEmail and handle profile picture visibility', () => {
        // With email and picture
        renderer.updateUserProfile({
            email: 'test@example.com',
            picture: 'http://pic.jpg',
            name: 'Test User'
        });
        const emailEl = document.getElementById('userEmail') as HTMLElement;
        expect(emailEl.classList.contains('hidden')).toBe(false);
        expect(emailEl.textContent).toBe('test@example.com');
        expect((document.getElementById('userPicture') as HTMLElement).classList.contains('hidden')).toBe(false);

        // Without picture
        renderer.updateUserProfile({
            email: 'test@example.com',
            picture: '',
            name: 'Test User'
        });
        expect((document.getElementById('userPicture') as HTMLElement).classList.contains('hidden')).toBe(true);
    });

    it('should not throw error if DOM elements are missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateUserProfile({ email: 'test@example.com', picture: 'http://pic.jpg', name: 'Test User' });
        }).not.toThrow();
    });
});

describe('renderer.js - updateServiceStatusUI', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="slackStatusLarge"></div>
            <div id="slackStatusText"></div>
            <div id="waQRSection"></div>
            <div id="waConnectedSection"></div>
            <div id="gmailConnectedInfo"></div>
            <div id="gmailDisconnectedInfo"></div>
        `;
    });

    it('should toggle active classes and sections via public methods', () => {
        // Slack Connected
        renderer.updateSlackStatus(true);
        expect((document.getElementById('slackStatusLarge') as HTMLElement).classList.contains('c-status-card--active')).toBe(true);

        // WhatsApp Connected
        renderer.updateWhatsAppStatus(true);
        expect((document.getElementById('waQRSection') as HTMLElement).classList.contains('hidden')).toBe(true);
        expect((document.getElementById('waConnectedSection') as HTMLElement).classList.contains('hidden')).toBe(false);

        // WhatsApp Disconnected
        renderer.updateWhatsAppStatus(false);
        expect((document.getElementById('waQRSection') as HTMLElement).classList.contains('hidden')).toBe(false);
        expect((document.getElementById('waConnectedSection') as HTMLElement).classList.contains('hidden')).toBe(true);
    });

    it('should not throw error when service status DOM is completely missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateSlackStatus(true);
            renderer.updateWhatsAppStatus(true);
        }).not.toThrow();
    });
});

// ── Helpers ──────────────────────────────────────────────────────────────────

function makeMsg(overrides: Partial<Message> & { id: number; task: string }): Message {
    return {
        requester: 'Alice',
        source: 'slack',
        done: false,
        ...overrides,
    } as Message;
}

const TASK_GRID_HTML = `
    <div id="receivedTasksList"></div>
    <div id="delegatedTasksList"></div>
    <div id="referenceTasksList"></div>
    <div id="allTasksList"></div>
    <span id="receivedCount">0</span>
    <span id="delegatedCount">0</span>
    <span id="referenceCount">0</span>
    <span id="allCount">0</span>
    <input id="taskSearch" type="text" value="" />
`;

// ─── renderEmptyGrid ────────────────────────────────────────────────────────

describe('renderer.js - renderEmptyGrid', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
    });

    it('renders plain empty-state markup when isWitty=false', () => {
        const grid = document.getElementById('receivedTasksList') as HTMLElement;
        renderer.renderEmptyGrid(grid, false);
        expect(grid.innerHTML).toContain('empty-state');
        expect(grid.innerHTML).toContain('📭');
    });

    it('renders witty variant without crashing when isWitty=true', () => {
        const grid = document.getElementById('receivedTasksList') as HTMLElement;
        renderer.renderEmptyGrid(grid, true);
        expect(grid.innerHTML).toContain('empty-state');
    });

    it('does not throw when grid is null', () => {
        expect(() => renderer.renderEmptyGrid(null)).not.toThrow();
    });

    it('clears previous content before rendering', () => {
        const grid = document.getElementById('receivedTasksList') as HTMLElement;
        grid.innerHTML = '<div class="stale">old</div>';
        renderer.renderEmptyGrid(grid, false);
        expect(grid.innerHTML).not.toContain('stale');
    });
});

// ─── renderMessages — count badges ──────────────────────────────────────────

describe('renderer.js - renderMessages count badges', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
        state.deadlineFilter = 'all';
    });

    it('shows correct active counts per category', () => {
        renderer.renderMessages({
            inbox:     [makeMsg({ id: 1, task: 'a' }), makeMsg({ id: 2, task: 'b' })],
            delegated: [makeMsg({ id: 3, task: 'c' })],
            reference: [],
        });
        expect(document.getElementById('receivedCount')?.textContent).toBe('2');
        expect(document.getElementById('delegatedCount')?.textContent).toBe('1');
        expect(document.getElementById('referenceCount')?.textContent).toBe('0');
        expect(document.getElementById('allCount')?.textContent).toBe('3');
    });

    it('excludes done tasks from active count', () => {
        renderer.renderMessages({
            inbox: [
                makeMsg({ id: 1, task: 'active' }),
                makeMsg({ id: 2, task: 'done', done: true }),
            ],
            delegated: [],
            reference: [],
        });
        expect(document.getElementById('receivedCount')?.textContent).toBe('1');
    });

    it('shows zero counts when all categories are empty', () => {
        renderer.renderMessages({ inbox: [], delegated: [], reference: [] });
        expect(document.getElementById('receivedCount')?.textContent).toBe('0');
        expect(document.getElementById('allCount')?.textContent).toBe('0');
    });
});

// ─── renderMessages — tab content ────────────────────────────────────────────

describe('renderer.js - renderMessages tab content', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
        state.deadlineFilter = 'all';
    });

    it('renders inbox into receivedTasksList', () => {
        renderer.renderMessages({ inbox: [makeMsg({ id: 10, task: 'inbox task' })], delegated: [], reference: [] });
        expect(document.getElementById('receivedTasksList')?.querySelector('.c-message-card[data-id="10"]')).not.toBeNull();
    });

    it('renders delegated into delegatedTasksList', () => {
        renderer.renderMessages({ inbox: [], delegated: [makeMsg({ id: 20, task: 'del task' })], reference: [] });
        expect(document.getElementById('delegatedTasksList')?.querySelector('.c-message-card[data-id="20"]')).not.toBeNull();
    });

    it('renders reference into referenceTasksList', () => {
        renderer.renderMessages({ inbox: [], delegated: [], reference: [makeMsg({ id: 30, task: 'ref task' })] });
        expect(document.getElementById('referenceTasksList')?.querySelector('.c-message-card[data-id="30"]')).not.toBeNull();
    });

    it('renders all combined in allTasksList', () => {
        renderer.renderMessages({
            inbox:     [makeMsg({ id: 1, task: 'i' })],
            delegated: [makeMsg({ id: 2, task: 'd' })],
            reference: [makeMsg({ id: 3, task: 'r' })],
        });
        const allGrid = document.getElementById('allTasksList') as HTMLElement;
        expect(allGrid.querySelector('.c-message-card[data-id="1"]')).not.toBeNull();
        expect(allGrid.querySelector('.c-message-card[data-id="2"]')).not.toBeNull();
        expect(allGrid.querySelector('.c-message-card[data-id="3"]')).not.toBeNull();
    });

    it('shows empty state for tabs with no messages', () => {
        renderer.renderMessages({ inbox: [makeMsg({ id: 1, task: 'x' })], delegated: [], reference: [] });
        expect(document.getElementById('delegatedTasksList')?.innerHTML).toContain('empty-state');
        expect(document.getElementById('referenceTasksList')?.innerHTML).toContain('empty-state');
    });
});

// ─── renderMessages — empty state ────────────────────────────────────────────

describe('renderer.js - renderMessages empty state', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
        state.deadlineFilter = 'all';
    });

    it('renders empty state for received tab when inbox is empty and no search term', () => {
        (document.getElementById('taskSearch') as HTMLInputElement).value = '';
        renderer.renderMessages({ inbox: [], delegated: [], reference: [] });
        expect(document.getElementById('receivedTasksList')?.innerHTML).toContain('empty-state');
    });
});

// ─── renderMessages — re-render idempotence ───────────────────────────────────

describe('renderer.js - renderMessages idempotence', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
        state.deadlineFilter = 'all';
    });

    it('does not duplicate cards on second render with same data', () => {
        const categorized = { inbox: [makeMsg({ id: 99, task: 'once' })], delegated: [], reference: [] };
        renderer.renderMessages(categorized);
        renderer.renderMessages(categorized);
        // Use .c-message-card selector to avoid matching inner inputs that also carry data-id
        const cards = document.getElementById('receivedTasksList')?.querySelectorAll('.c-message-card[data-id="99"]');
        expect(cards?.length).toBe(1);
    });
});

// ─── renderMessages — skipClientSearch ───────────────────────────────────────

describe('renderer.js - renderMessages skipClientSearch', () => {
    beforeEach(() => {
        document.body.innerHTML = TASK_GRID_HTML;
        state.deadlineFilter = 'all';
    });

    it('bypasses client-side search when skipClientSearch=true', () => {
        (document.getElementById('taskSearch') as HTMLInputElement).value = 'xyz-no-match';
        renderer.renderMessages(
            { inbox: [makeMsg({ id: 5, task: 'completely different text' })], delegated: [], reference: [] },
            { skipClientSearch: true },
        );
        expect(document.getElementById('receivedTasksList')?.querySelector('.c-message-card[data-id="5"]')).not.toBeNull();
    });

    it('applies client-side search filter when skipClientSearch not set', () => {
        (document.getElementById('taskSearch') as HTMLInputElement).value = 'xyz-no-match';
        renderer.renderMessages(
            { inbox: [makeMsg({ id: 6, task: 'completely different text' })], delegated: [], reference: [] },
        );
        expect(document.getElementById('receivedTasksList')?.querySelector('.c-message-card[data-id="6"]')).toBeNull();
    });
});

// ─── renderArchive ────────────────────────────────────────────────────────────

describe('renderer.js - renderArchive', () => {
    beforeEach(() => {
        // Why: raw <tbody> as body-direct-child is normalized away by happy-dom; wrap in <table>.
        document.body.innerHTML = '<table><tbody id="archiveBody"></tbody></table>';
    });

    it('renders one row per message', () => {
        renderer.renderArchive([
            makeMsg({ id: 1, task: 'Task A', source: 'slack' }),
            makeMsg({ id: 2, task: 'Task B', source: 'gmail' }),
        ]);
        expect(document.getElementById('archiveBody')?.querySelectorAll('tr').length).toBe(2);
    });

    it('renders no-data row when list is empty', () => {
        renderer.renderArchive([]);
        expect(document.getElementById('archiveBody')?.innerHTML).toContain('No archived messages');
    });

    it('does not throw when archiveBody element is absent', () => {
        document.body.innerHTML = '';
        expect(() => renderer.renderArchive([makeMsg({ id: 1, task: 't' })])).not.toThrow();
    });

    it('marks deleted messages with archive-row-deleted class', () => {
        renderer.renderArchive([makeMsg({ id: 1, task: 'gone', is_deleted: true })]);
        expect(document.getElementById('archiveBody')?.innerHTML).toContain('archive-row-deleted');
    });

    it('escapes task text to prevent XSS', () => {
        renderer.renderArchive([makeMsg({ id: 1, task: '<script>alert(1)</script>' })]);
        // After DOM parsing, innerHTML may decode entities — verify no executable script element
        const body = document.getElementById('archiveBody') as HTMLElement;
        expect(body.querySelectorAll('script').length).toBe(0);
        // textContent should carry the literal angle-bracket text without executing
        expect(body.textContent).toContain('<script>');
    });
});

// ─── removeTaskNode ───────────────────────────────────────────────────────────

describe('renderer.js - removeTaskNode', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        document.body.innerHTML = `
            <div id="receivedTasksList">
                <div class="c-message-card" data-id="7">Task 7</div>
                <div class="c-message-card" data-id="8">Task 8</div>
            </div>
            <span id="receivedCount">2</span>
            <span id="delegatedCount">0</span>
            <span id="referenceCount">0</span>
            <span id="allCount">2</span>
        `;
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('adds removing class immediately', () => {
        renderer.removeTaskNode(7);
        expect((document.querySelector('[data-id="7"]') as HTMLElement).classList.contains('c-message-card--removing')).toBe(true);
    });

    it('removes card from DOM after 300ms', () => {
        renderer.removeTaskNode(7);
        vi.advanceTimersByTime(300);
        expect(document.querySelector('[data-id="7"]')).toBeNull();
    });

    it('does not throw when card id is not found', () => {
        expect(() => renderer.removeTaskNode(999)).not.toThrow();
    });

    it('shows empty state when last card is removed', () => {
        document.body.innerHTML = `
            <div id="receivedTasksList">
                <div class="c-message-card" data-id="7">Task 7</div>
            </div>
            <span id="receivedCount">1</span>
            <span id="delegatedCount">0</span>
            <span id="referenceCount">0</span>
            <span id="allCount">1</span>
        `;
        renderer.removeTaskNode(7);
        vi.advanceTimersByTime(300);
        expect(document.getElementById('receivedTasksList')?.innerHTML).toContain('empty-state');
    });
});

// ─── updateTaskNodeStatus ─────────────────────────────────────────────────────

describe('renderer.js - updateTaskNodeStatus', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div class="c-message-card" data-id="5">
                <button class="toggle-done-btn">✅</button>
            </div>
            <span id="receivedCount">0</span>
            <span id="delegatedCount">0</span>
            <span id="referenceCount">0</span>
            <span id="allCount">0</span>
        `;
    });

    it('adds done class when done=true', () => {
        renderer.updateTaskNodeStatus(5, true);
        expect((document.querySelector('[data-id="5"]') as HTMLElement).classList.contains('c-message-card--done')).toBe(true);
    });

    it('removes done class when done=false', () => {
        const card = document.querySelector('[data-id="5"]') as HTMLElement;
        card.classList.add('c-message-card--done');
        renderer.updateTaskNodeStatus(5, false);
        expect(card.classList.contains('c-message-card--done')).toBe(false);
    });

    it('updates toggle button to ↩️ when done', () => {
        renderer.updateTaskNodeStatus(5, true);
        expect((document.querySelector('.toggle-done-btn') as HTMLElement).innerHTML).toBe('↩️');
    });

    it('updates toggle button to ✅ when not done', () => {
        renderer.updateTaskNodeStatus(5, false);
        expect((document.querySelector('.toggle-done-btn') as HTMLElement).innerHTML).toBe('✅');
    });

    it('does not throw when card not found', () => {
        expect(() => renderer.updateTaskNodeStatus(9999, true)).not.toThrow();
    });
});

// ─── updateSubtaskNodeStatus ──────────────────────────────────────────────────

describe('renderer.js - updateSubtaskNodeStatus', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div class="c-message-card" data-id="10">
                <li class="c-message-card__subtask-item"><span class="c-message-card__subtask-check">•</span></li>
                <li class="c-message-card__subtask-item"><span class="c-message-card__subtask-check">•</span></li>
            </div>
        `;
    });

    it('adds done class to targeted subtask only', () => {
        renderer.updateSubtaskNodeStatus(10, 0, true);
        const items = document.querySelectorAll('.c-message-card__subtask-item');
        expect(items[0].classList.contains('c-message-card__subtask-item--done')).toBe(true);
        expect(items[1].classList.contains('c-message-card__subtask-item--done')).toBe(false);
    });

    it('updates check to ✅ when done', () => {
        renderer.updateSubtaskNodeStatus(10, 1, true);
        const checks = document.querySelectorAll('.c-message-card__subtask-check');
        expect(checks[1].textContent).toBe('✅');
    });

    it('updates check to • when not done', () => {
        renderer.updateSubtaskNodeStatus(10, 0, false);
        expect(document.querySelectorAll('.c-message-card__subtask-check')[0].textContent).toBe('•');
    });

    it('does not throw when task card is not found', () => {
        expect(() => renderer.updateSubtaskNodeStatus(9999, 0, true)).not.toThrow();
    });
});

// ─── updateMessageCard ────────────────────────────────────────────────────────

describe('renderer.js - updateMessageCard', () => {
    beforeEach(() => {
        state.currentLang = 'ko';
        document.body.innerHTML = `
            <div id="receivedTasksList">
                <div class="c-message-card" data-id="42" id="task-42">stale</div>
            </div>
            <div id="allTasksList">
                <div class="c-message-card" data-id="42" id="task-42">stale</div>
            </div>
        `;
    });

    it('repaints every copy of the card across grids (duplicate data-id)', () => {
        // Display path renders m.task (backend overwrites task with the translation).
        const msg = makeMsg({ id: 42, task: '번역된 작업' });
        renderer.updateMessageCard(msg);
        const cards = document.querySelectorAll('.c-message-card[data-id="42"]');
        expect(cards.length).toBe(2);
        cards.forEach(card => {
            expect(card.textContent).not.toContain('stale');
            expect(card.textContent).toContain('번역된 작업');
        });
    });

    it('renders the merged translation text in place of the stale card', () => {
        const msg = makeMsg({ id: 42, task: '한국어 번역' });
        renderer.updateMessageCard(msg);
        const card = document.querySelector('.c-message-card[data-id="42"]') as HTMLElement;
        expect(card.textContent).toContain('한국어 번역');
        expect(card.textContent).not.toContain('stale');
    });

    it('clears the translating spinner once translation is merged', () => {
        // Card starts in translating state (spinner present)
        renderer.updateMessageCard(makeMsg({ id: 42, task: 'orig', is_translating: true }));
        expect(document.querySelector('.c-message-card[data-id="42"]')?.innerHTML).toContain('translating-badge');
        // Translation arrives → spinner gone
        renderer.updateMessageCard(makeMsg({ id: 42, task: 'orig', task_ko: 'done', is_translating: false }));
        document.querySelectorAll('.c-message-card[data-id="42"]').forEach(card => {
            expect(card.innerHTML).not.toContain('translating-badge');
        });
    });

    it('does not throw when card is not in the DOM', () => {
        expect(() => renderer.updateMessageCard(makeMsg({ id: 9999, task: 'ghost' }))).not.toThrow();
    });
});

// ─── initMessageGridEvents ────────────────────────────────────────────────────

describe('renderer.js - initMessageGridEvents', () => {
    it('does not throw when grid element does not exist', () => {
        expect(() => renderer.initMessageGridEvents('nonexistent', {
            onToggleDone: vi.fn(),
            onDeleteTask: vi.fn(),
            onShowOriginal: vi.fn(),
        })).not.toThrow();
    });

    it('calls onToggleDone when toggle-done action button is clicked', async () => {
        document.body.innerHTML = `
            <div id="evtGrid">
                <div class="c-message-card" data-id="1">
                    <button data-action="toggle-done">✅</button>
                </div>
            </div>
        `;
        const onToggleDone = vi.fn().mockResolvedValue(undefined);
        renderer.initMessageGridEvents('evtGrid', {
            onToggleDone,
            onDeleteTask: vi.fn().mockResolvedValue(undefined),
            onShowOriginal: vi.fn().mockResolvedValue(undefined),
        });
        (document.querySelector('[data-action="toggle-done"]') as HTMLElement).click();
        await Promise.resolve();
        expect(onToggleDone).toHaveBeenCalledWith('1', true);
    });

    it('calls onDeleteTask when delete action button is clicked', async () => {
        document.body.innerHTML = `
            <div id="evtGrid2">
                <div class="c-message-card" data-id="2">
                    <button data-action="delete">🗑️</button>
                </div>
            </div>
        `;
        const onDeleteTask = vi.fn().mockResolvedValue(undefined);
        renderer.initMessageGridEvents('evtGrid2', {
            onToggleDone: vi.fn().mockResolvedValue(undefined),
            onDeleteTask,
            onShowOriginal: vi.fn().mockResolvedValue(undefined),
        });
        (document.querySelector('[data-action="delete"]') as HTMLElement).click();
        await Promise.resolve();
        expect(onDeleteTask).toHaveBeenCalledWith('2');
    });
});

// ─── getVisibleUntranslatedIds ────────────────────────────────────────────────

describe('renderer.js - getVisibleUntranslatedIds', () => {
    it('returns empty array when no active tab panel exists', () => {
        document.body.innerHTML = '<div>no panels</div>';
        expect(renderer.getVisibleUntranslatedIds()).toEqual([]);
    });

    it('returns empty array when lang is English', () => {
        state.currentLang = 'en';
        document.body.innerHTML = `
            <div class="c-tabs__panel active">
                <div class="c-message-card" id="task-1"></div>
            </div>
        `;
        expect(renderer.getVisibleUntranslatedIds()).toEqual([]);
    });

    it('returns id for card needing translation in non-English lang', () => {
        state.currentLang = 'ko';
        const msg = makeMsg({ id: 55, task: 'needs translation' });
        state.messages = { inbox: [msg], delegated: [], reference: [] };
        document.body.innerHTML = `
            <div class="c-tabs__panel active">
                <div class="c-message-card" id="task-55"></div>
            </div>
        `;
        const ids = renderer.getVisibleUntranslatedIds();
        expect(ids).toContain(55);
    });
});
