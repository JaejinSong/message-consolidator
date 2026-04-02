import { MessageCard } from './components/message-card.ts';
import { sortAndFilterMessages, classifyMessages } from './logic.ts';
import { state } from './state.ts';
import { I18N_DATA } from './locales.js';
import { TimeService, escapeHTML } from './utils.ts';
import { ICONS } from './icons.ts';
import { Message, I18nDictionary, MessageHandlers } from './types.ts';

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
} from './renderers/status-renderer.ts';

export { updateUserProfile } from './renderers/profile-renderer.ts';

export { 
    renderAliasList, 
    renderTenantAliasList, 
    renderContactMappings,
    renderLinkedAccounts,
    initAccountLinkingCompos
} from './renderers/settings-renderer.ts';

export { 
    triggerXPAnimation, 
    triggerConfetti, 
    showToast, 
    renderReleaseNotes, 
    setScanLoading, 
    setTheme, 
    bindThemeToggle 
} from './renderers/ui-effects.ts';

/**
 * @file renderer.ts
 * @description Central aggregator for UI rendering logic, migrated to TypeScript.
 */

/**
 * Renders an empty grid state when no tasks are found.
 */
export function renderEmptyGrid(grid: HTMLElement | null, isWitty: boolean = false): void {
    if (!grid) return;
    const lang = state.currentLang || 'ko';
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
        const btn = target.closest('button[data-action]');
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
        lang: state.currentLang || 'ko',
        translating: !!m.translating,
        translationError: (m.translationError || undefined) as string | undefined, 
        has_original: !!m.has_original
    };

    return MessageCard(props);
}

/**
 * Renders message cards based on data and current state.
 */
export function renderMessages(messages: Message[], handlers: MessageHandlers): void {
    const activeTab = document.querySelector('.tab-btn.active');
    const currentTab = activeTab?.getAttribute('data-tab') || 'myTasksTab';
    const searchInput = document.getElementById('taskSearch') as HTMLInputElement | null;
    const searchQuery = searchInput?.value || '';

    const filtered = sortAndFilterMessages(messages, currentTab, searchQuery);
    const counts = classifyMessages(messages);

    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };
    updateCount('myCount', counts.my);
    updateCount('otherCount', counts.others);
    updateCount('waitingCount', counts.waiting);
    updateCount('allCount', counts.all);

    const gridId = currentTab.replace('Tab', 'List');
    const grid = document.getElementById(gridId);
    if (!grid) return;

    const isMyTasksDone = (currentTab === 'myTasksTab' && counts.my === 0 && !searchQuery);

    if (isMyTasksDone) {
        renderEmptyGrid(grid, true);
        if (filtered.length > 0) {
            const listHtml = filtered.map(m => createCardElement(m)).join('');
            grid.insertAdjacentHTML('beforeend', `<div class="completed-list-divider"></div>` + listHtml);
        }
    } else if (filtered.length === 0) {
        renderEmptyGrid(grid, false);
    } else {
        grid.innerHTML = filtered.map(m => createCardElement(m)).join('');
    }
}

/**
 * Renders archived messages in the archive table.
 */
export function renderArchive(messages: Message[]): void {
    const tableBody = document.getElementById('archiveBody');
    if (!tableBody) return;

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
