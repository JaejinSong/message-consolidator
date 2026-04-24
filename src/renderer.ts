import { MessageCard } from './components/message-card';
import { Message, I18nDictionary, MessageHandlers, CategorizedMessages } from './types';
import { sortAndSearchMessages, getActiveCount } from './logic';
import { state } from './state';
import { I18N_DATA } from './locales';
import { TimeService, escapeHTML } from './utils';
import { ICONS } from './icons';


export {
    updateServiceStatusUI,
    updateSlackStatus,
    updateWhatsAppStatus,
    updateGmailStatus,
    showWaModal,
    showGmailModal,
    bindGetQRBtn,
    updateWhatsAppQR,
    updateQRTimer,
    bindGmailStatus,
    bindWhatsAppStatus
} from './renderers/status-renderer';

export { updateUserProfile } from './renderers/profile-renderer';

export {
    renderProposals
} from './renderers/settings-renderer';

export {
    showToast,
    renderReleaseNotes,
    setScanLoading,
    setTheme,
    bindThemeToggle
} from './renderers/ui-effects';

/**
 * @file renderer.ts
 * @description Central aggregator for UI rendering logic, migrated to TypeScript.
 */

/**
 * Renders an empty grid state when no tasks are found.
 */
export function renderEmptyGrid(grid: HTMLElement | null, isWitty: boolean = false): void {
    if (!grid) return;
    grid.innerHTML = '';
    const lang = state.currentLang || 'en';
    const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
    const messages = i18n.emptyStateMessages;
    let displayMsg = i18n.noTasks || 'No tasks found';

    if (isWitty && messages && messages.length > 0) {
        const randomIndex = Math.floor(Math.random() * messages.length);
        displayMsg = messages[randomIndex];
        grid.innerHTML = `
            <div class="empty-state-witty">
                <div class="empty-state__icon">✨</div>
                <div class="empty-state__message">${displayMsg}</div>
            </div>
        `;
    } else {
        grid.innerHTML = `
            <div class="empty-state">
                <div class="empty-state__icon">📭</div>
                <div class="empty-state__message">${displayMsg}</div>
            </div>
        `;
    }
}

/**
 * Why: Standard Event Delegation. Attaches a single listener to the grid container
 * to handle all button clicks based on data-action and data-id.
 */
export function initMessageGridEvents(gridId: string, handlers: MessageHandlers): void {
    const grid = document.getElementById(gridId);
    if (!grid) return;

    grid.addEventListener('click', async (e) => {
        const target = e.target as HTMLElement;
        const btn = target.closest('[data-action]');
        if (!btn) return;

        const action = btn.getAttribute('data-action');
        const card = btn.closest('.c-message-card');
        const id = card?.getAttribute('data-id');

        if (!id) return;

        switch (action) {
            case 'toggle-done':
                const isDone = card?.classList.contains('c-message-card--done');
                await handlers.onToggleDone(id, !isDone);
                break;
            case 'toggle-subtask':
                const index = parseInt(btn.getAttribute('data-index') || '0', 10);
                const isSubtaskDone = btn.classList.contains('c-message-card__subtask-item--done');
                if (handlers.onToggleSubtask) {
                    await handlers.onToggleSubtask(id, index, !isSubtaskDone);
                }
                break;
            case 'delete':
                await handlers.onDeleteTask(id);
                break;
            case 'show-original':
                await handlers.onShowOriginal(id);
                break;
            case 'map-alias':
                const name = btn.getAttribute('data-name');
                const source = btn.getAttribute('data-source');
                if (name && source && handlers.onMapAlias) {
                    handlers.onMapAlias(name, source);
                }
                break;
            case 'select-task':
                // Checkboxes toggle their native .checked state BEFORE the click event fires.
                // We MUST NOT call e.preventDefault() here, otherwise the native toggle is reverted.
                e.stopPropagation(); // Only prevent bubbling so the card doesn't expand

                const checkbox = btn as HTMLInputElement;
                const taskId = parseInt(id, 10);
                const isSelectedNow = checkbox.checked;

                console.log(`[DEBUG] Task ${taskId} Clicked. isSelectedNow=${isSelectedNow}`);

                if (handlers.onSelectTask) {
                    handlers.onSelectTask(taskId, isSelectedNow);
                }
                break;
        }
    });
}

/**
 * Creates HTML string for a single message card using the MessageCard component.
 */
export function createCardElement(m: Message): string {
    const props = {
        id: m.id,
        user_email: m.user_email || '',
        source: m.source,
        room: m.room || '',
        task: m.task,
        requester: m.requester,
        assignee: m.assignee || '',
        timestamp: m.timestamp,
        created_at: m.created_at,
        link: m.link || '',
        source_ts: m.source_ts || '',
        done: !!m.done,
        category: m.category || 'TASK',
        metadata: typeof m.metadata === 'string' ? m.metadata : JSON.stringify(m.metadata || {}),
        lang: state.currentLang || 'en',
        is_translating: !!m.is_translating,
        translation_error: (m.translation_error || undefined) as string | undefined,
        has_original: !!m.has_original,
        assigned_to: m.assigned_to,
        isSelected: state.selectedTaskIds.has(m.id),
        currentUserNames: [state.userProfile.name, ...(state.userProfile.aliases || [])].filter(Boolean),
        deadline: m.deadline || '',
        source_channels: m.source_channels,
        consolidated_context: m.consolidated_context,
        subtasks: m.subtasks,
    };

    return MessageCard(props);
}

import { filterByDeadline, TaskTab } from './taskFilter';

/**
 * Renders message cards based on categorized data.
 */
export function renderMessages(categorized: CategorizedMessages): void {
    const searchQuery = (document.getElementById('taskSearch') as HTMLInputElement)?.value || '';
    const dlFilter = state.deadlineFilter || 'all';

    const inbox     = categorized.inbox     || [];
    const delegated = categorized.delegated || [];
    const reference = categorized.reference || [];
    const allTasks  = [...inbox, ...delegated, ...reference];

    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };

    updateCount('receivedCount',  getActiveCount(inbox));
    updateCount('delegatedCount', getActiveCount(delegated));
    updateCount('referenceCount', getActiveCount(reference));
    updateCount('allCount',       getActiveCount(allTasks));

    const gridsConfig: { tab: TaskTab, gridId: string, messages: Message[] }[] = [
        { tab: 'received',  gridId: 'receivedTasksList',  messages: filterByDeadline(inbox, dlFilter) },
        { tab: 'delegated', gridId: 'delegatedTasksList', messages: filterByDeadline(delegated, dlFilter) },
        { tab: 'reference', gridId: 'referenceTasksList', messages: filterByDeadline(reference, dlFilter) },
        {
            tab: 'all',
            gridId: 'allTasksList',
            messages: filterByDeadline(allTasks, dlFilter).sort((a, b) => {
                const dateA = new Date(a.timestamp || a.created_at || 0).getTime();
                const dateB = new Date(b.timestamp || b.created_at || 0).getTime();
                return dateB - dateA;
            })
        }
    ];

    gridsConfig.forEach(config => {
        const grid = document.getElementById(config.gridId);
        if (!grid) return;

        const filtered = sortAndSearchMessages(config.messages, searchQuery);
        const isMyTasksEmpty = (config.tab === 'received' && config.messages.length === 0 && !searchQuery);

        grid.innerHTML = '';
        if (isMyTasksEmpty) {
            renderEmptyGrid(grid, true);
        } else if (filtered.length === 0) {
            renderEmptyGrid(grid, false);
        } else {
            grid.innerHTML = filtered.map(m => createCardElement(m)).join('');
        }
    });
}

/**
 * Why: Returns IDs of untranslated messages currently visible in the dashboard.
 */
export const getVisibleUntranslatedIds = (): number[] => {
    // Check which tab content is currently active
    const activeTab = document.querySelector('.c-tabs__panel.active') || document.querySelector('.c-tabs__panel.c-tabs__panel--active');
    if (!activeTab) return [];

    const targetLang = state.currentLang || 'en';
    if (targetLang === 'en') return [];

    // Find all message cards in the active tab that need translation
    const cards = Array.from(activeTab.querySelectorAll('.c-message-card'));
    const ids: number[] = [];

    cards.forEach(card => {
        const idStr = card.id.replace('task-', '');
        const id = parseInt(idStr);
        if (isNaN(id)) return;

        // Use global state to check if it needs translation
        const all = [...state.messages.inbox, ...state.messages.delegated, ...state.messages.reference];
        const m = all.find(item => item.id === id);

        if (m && !m.task_ko && !m.is_translating) {
            ids.push(id);
        }
    });

    return ids;
}

/**
 * Why: Optimistically removes a task card from the DOM with animation.
 * If the list becomes empty, it renders the empty state.
 */
export function removeTaskNode(id: number): void {
    const cards = document.querySelectorAll(`.c-message-card[data-id="${id}"]`);
    if (cards.length === 0) return;

    cards.forEach(card => {
        const grid = card.parentElement;
        card.classList.add('c-message-card--removing');

        // Wait for animation to finish before removal
        setTimeout(() => {
            card.remove();

            // If grid is now empty, show empty state
            if (grid && grid.children.length === 0) {
                renderEmptyGrid(grid as HTMLElement, grid.id === 'receivedTasksList');
            }
        }, 300);
    });

    setTimeout(() => {
        const allTasks = [...state.messages.inbox, ...state.messages.delegated, ...state.messages.reference];
        const updateCount = (countId: string, count: number) => {
            const el = document.getElementById(countId);
            if (el) el.textContent = count.toString();
        };
        updateCount('receivedCount',  getActiveCount(state.messages.inbox));
        updateCount('delegatedCount', getActiveCount(state.messages.delegated));
        updateCount('referenceCount', getActiveCount(state.messages.reference));
        updateCount('allCount',       getActiveCount(allTasks));
    }, 300);
}

/**
 * Why: Optimistically updates a task card's completion status without full re-render.
 */
export function updateTaskNodeStatus(id: number, done: boolean): void {
    const cards = document.querySelectorAll(`.c-message-card[data-id="${id}"]`);
    if (cards.length === 0) return;

    cards.forEach(card => {
        card.classList.toggle('c-message-card--done', done);

        // Update the toggle button icon
        const btn = card.querySelector('.toggle-done-btn');
        if (btn) {
            btn.innerHTML = done ? '↩️' : '✅';
        }
    });

    const allTasks = [...state.messages.inbox, ...state.messages.delegated, ...state.messages.reference];
    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };
    updateCount('receivedCount',  getActiveCount(state.messages.inbox));
    updateCount('delegatedCount', getActiveCount(state.messages.delegated));
    updateCount('referenceCount', getActiveCount(state.messages.reference));
    updateCount('allCount',       getActiveCount(allTasks));
}

/**
 * Renders archived messages in the archive table.
 */
export function renderArchive(messages: Message[]): void {
    const tableBody = document.getElementById('archiveBody');
    if (!tableBody) return;
    tableBody.innerHTML = '';

    if (!messages || messages.length === 0) {
        tableBody.innerHTML = '<tr><td colspan="8" class="empty-state">No archived messages</td></tr>';
        return;
    }

    tableBody.innerHTML = messages.map(m => {
        const sourceIcon = m.source === 'slack' ? ICONS.slack : m.source === 'whatsapp' ? ICONS.whatsapp : ICONS.gmail;
        const ts = m.timestamp || m.created_at || '';
        const compTs = m.completed_at || '-';

        const assigneeHtml = `<span class="c-badge c-badge--dim">${escapeHTML(m.assignee || '-')}</span>`;

        const isDeleted = (m as any).is_deleted === true || (m as any).is_deleted === 1;
        const trashIcon = isDeleted ? `<span class="trash-icon" title="${state.currentLang === 'ko' ? '취소함' : 'Canceled'}">🗑️</span> ` : '';
        const rowClass = isDeleted ? 'archive-row-deleted' : '';

        return `
            <tr class="${rowClass}">
                <td><input type="checkbox" class="archive-check" data-id="${m.id}"></td>
                <td>
                    <div class="c-archive-table__source" title="${m.source.toUpperCase()}">
                        ${sourceIcon}
                    </div>
                </td>
                <td>${m.room ? `<span class="c-badge c-badge--accent" style="background: rgba(var(--color-primary-rgb), 0.1);">${escapeHTML(m.room)}</span>` : '-'}</td>
                <td class="c-archive-table__task">${trashIcon}${escapeHTML(m.task)}</td>
                <td class="c-archive-table__meta">${escapeHTML(m.requester)}</td>
                <td class="c-archive-table__meta">${assigneeHtml}</td>
                <td class="c-archive-table__meta">${TimeService.formatDisplayTime(ts, state.currentLang)}</td>
                <td class="c-archive-table__meta">${compTs !== '-' ? TimeService.formatDisplayTime(compTs, state.currentLang) : '-'}</td>
            </tr>
        `;
    }).join('');
}

/**
 * Binds global click events.
 */
export function bindGlobalClicks(_handlers: {}): void {
    document.body.addEventListener('click', () => {
        // Placeholder for future global click handlers
    });
}

/**
 * Why: Optimistically updates a subtask's completion status without full re-render.
 */
export function updateSubtaskNodeStatus(taskId: number, index: number, done: boolean): void {
    const cards = document.querySelectorAll(`.c-message-card[data-id="${taskId}"]`);
    cards.forEach(card => {
        const subtaskEls = card.querySelectorAll('.c-message-card__subtask-item');
        const item = subtaskEls[index];
        if (item) {
            item.classList.toggle('c-message-card__subtask-item--done', done);
            const check = item.querySelector('.c-message-card__subtask-check');
            if (check) check.textContent = done ? '✅' : '•';
        }
    });
}
