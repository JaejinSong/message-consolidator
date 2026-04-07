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
    renderAliasList, 
    renderTenantAliasList, 
    renderContactMappings,
    renderLinkedAccounts,
    initAccountLinkingCompos
} from './renderers/settings-renderer';

export { 
    triggerXPAnimation, 
    triggerConfetti, 
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
        translating: !!m.translating,
        translationError: (m.translationError || undefined) as string | undefined, 
        has_original: !!m.has_original,
        assigned_to: m.assigned_to,
        isSelected: state.selectedTaskIds.has(m.id)
    };

    return MessageCard(props);
}

/**
 * Renders message cards based on categorized data.
 */
export function renderMessages(categorized: CategorizedMessages): void {
    const activeTab = document.querySelector('.tab-btn.active');
    const currentTab = activeTab?.getAttribute('data-tab') || 'myTasksTab';
    const searchQuery = (document.getElementById('taskSearch') as HTMLInputElement)?.value || '';

    const allTasks = [...(categorized.inbox || []), ...(categorized.pending || []), ...(categorized.waiting || [])];
    
    const tabToKey: Record<string, keyof CategorizedMessages> = {
        'myTasksTab': 'inbox',
        'otherTasksTab': 'pending',
        'waitingTasksTab': 'waiting'
    };

    let messages: Message[] = [];
    if (currentTab === 'allTasksTab') {
        messages = [...allTasks].sort((a, b) => {
            const dateA = new Date(a.source_ts || a.timestamp || 0).getTime();
            const dateB = new Date(b.source_ts || b.timestamp || 0).getTime();
            return dateB - dateA;
        });
    } else {
        const key = tabToKey[currentTab] as keyof CategorizedMessages;
        messages = categorized[key] || [];
    }

    const filtered = sortAndSearchMessages(messages, searchQuery);

    // O(1) Counter Updates based on categorized data lengths
    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };

    updateCount('myCount', getActiveCount(categorized.inbox));
    updateCount('otherCount', getActiveCount(categorized.pending));
    updateCount('waitingCount', getActiveCount(categorized.waiting));
    updateCount('allCount', getActiveCount(allTasks));

    const gridId = currentTab.replace('Tab', 'List');
    const grid = document.getElementById(gridId);
    if (!grid) return;
    grid.innerHTML = '';

    const isMyTasksEmpty = (currentTab === 'myTasksTab' && messages.length === 0 && !searchQuery);

    if (isMyTasksEmpty) {
        renderEmptyGrid(grid, true);
    } else if (filtered.length === 0) {
        renderEmptyGrid(grid, false);
    }
}

/**
 * Why: Returns IDs of untranslated messages currently visible in the dashboard.
 */
export const getVisibleUntranslatedIds = (): number[] => {
    // Check which tab content is currently active
    const activeTab = document.querySelector('.c-tab-content.active');
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
        const all = [...state.messages.inbox, ...state.messages.pending, ...state.messages.waiting];
        const m = all.find(item => item.id === id);
        
        if (m && !m.task_ko && !m.translating) {
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
    const card = document.getElementById(`task-${id}`);
    if (!card) return;

    const grid = card.parentElement;
    card.classList.add('c-message-card--removing');

    // Wait for animation to finish before removal
    setTimeout(() => {
        card.remove();
        
        // If grid is now empty, show empty state
        if (grid && grid.children.length === 0) {
            renderEmptyGrid(grid, true);
        }
        
        // Update global counts in UI
        const allTasks = [...state.messages.inbox, ...state.messages.pending, ...state.messages.waiting];
        const updateCount = (id: string, count: number) => {
            const el = document.getElementById(id);
            if (el) el.textContent = count.toString();
        };
        updateCount('myCount', getActiveCount(state.messages.inbox));
        updateCount('otherCount', getActiveCount(state.messages.pending));
        updateCount('waitingCount', getActiveCount(state.messages.waiting));
        updateCount('allCount', getActiveCount(allTasks));
    }, 300);
}

/**
 * Why: Optimistically updates a task card's completion status without full re-render.
 */
export function updateTaskNodeStatus(id: number, done: boolean): void {
    const card = document.getElementById(`task-${id}`);
    if (!card) return;

    card.classList.toggle('c-message-card--done', done);
    
    // Update the toggle button icon
    const btn = card.querySelector('.toggle-done-btn');
    if (btn) {
        btn.innerHTML = done ? '↩️' : '✅';
    }
    
    // Update global counts
    const allTasks = [...state.messages.inbox, ...state.messages.pending, ...state.messages.waiting];
    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };
    updateCount('myCount', getActiveCount(state.messages.inbox));
    updateCount('otherCount', getActiveCount(state.messages.pending));
    updateCount('waitingCount', getActiveCount(state.messages.waiting));
    updateCount('allCount', getActiveCount(allTasks));
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

        const isMe = m.assignee?.toLowerCase() === 'me';
        const assigneeHtml = isMe
            ? `<span class="c-badge c-badge--accent">${escapeHTML(m.assignee || '')}</span>`
            : `<span class="c-badge c-badge--dim">${escapeHTML(m.assignee || '-')}</span>`;

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
 * Binds the scan button click event.
 */
export function bindScanBtn(onClick: (ev: MouseEvent) => void): void {
    document.getElementById('scanBtn')?.addEventListener('click', onClick);
}

/**
 * Binds global click events.
 */
export function bindGlobalClicks(handlers: { onBuyFreeze?: () => void }): void {
    document.body.addEventListener('click', (e) => {
        const target = e.target as HTMLElement | null;
        if (target?.closest('#buyFreezeBtn')) {
            if (handlers.onBuyFreeze) handlers.onBuyFreeze();
        }
    });
}
