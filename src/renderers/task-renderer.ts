import { state } from '../state.js';
import { I18N_DATA } from '../locales.js';
import { TimeService, escapeHTML } from '../utils.js';
import { sortAndFilterMessages, classifyMessages, getDeadlineBadge } from '../logic.js';
import { ICONS } from '../icons.js';

/**
 * Renders an empty grid state when no tasks are found.
 */
export function renderEmptyGrid(grid: HTMLElement | null, isWitty = false) {
    if (grid) {
        const lang = state.currentLang || 'ko';
        const messages = (I18N_DATA as any)[lang]?.emptyStateMessages;
        let displayMsg = (I18N_DATA as any)[lang]?.noTasks || 'No tasks found';

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
}

/**
 * Creates HTML string for a single message card.
 */
export function createCardElement(m: any) {
    const lang = state.currentLang;
    const i18n = (I18N_DATA as any)[lang];
    const ts = m.timestamp || m.created_at;
    const displayTime = TimeService.formatDisplayTime(ts, lang);
    const deadlineBadge = getDeadlineBadge(ts, m.done, lang || 'ko');

    const sourceIcon = m.source === 'slack' ? ICONS.slack : m.source === 'whatsapp' ? ICONS.whatsapp : ICONS.gmail;

    const meText = i18n?.assigneeMe || 'Me';
    const isMe = m.assignee === 'me';
    const isInvalid = !m.assignee || m.assignee === 'undefined' || m.assignee === 'unknown';
    const assigneeText = isMe
        ? `<span class="assignee-me">${meText}</span>`
        : `<span class="assignee-other">${isInvalid ? '' : escapeHTML(m.assignee)}</span>`;

    const isTranslating = !!m.translating;
    const translationError = m.translationError;

    const loadingOverlay = isTranslating ? `
        <div class="c-task-card__loading-overlay">
            <div class="c-spinner c-spinner--sm"></div>
        </div>
    ` : '';

    const errorIndicator = translationError ? `
        <span class="c-task-card__error-hint" title="${escapeHTML(translationError)}">⚠️</span>
    ` : '';

    return `
        <div class="c-task-card ${m.source} ${m.done ? 'c-task-card--done' : ''} ${isTranslating ? 'c-task-card--loading' : ''}" id="task-${m.id}" data-id="${m.id}">
            ${loadingOverlay}
            <div class="c-task-card__source" title="${m.source.toUpperCase()}">
                ${sourceIcon}
            </div>
            <div class="c-task-card__room">${m.room ? `<span class="badge-room">${escapeHTML(m.room)}</span>` : '-'}</div>
            <div class="c-task-card__content">
                <span class="c-task-card__title">${errorIndicator}${escapeHTML(m.task)}</span>
                <div class="c-task-card__tags">
                    ${m.category === 'waiting' && !m.done ? `<span class="tag-badge waiting-tag">⏳ ${(i18n as any).waitingTag || 'Waiting...'}</span>` : ''}
                    ${m.category === 'promise' && !m.done ? `<span class="tag-badge promise-tag">🤝 ${(i18n as any).promiseTag || 'Commitment'}</span>` : ''}
                </div>
            </div>
            <div class="c-task-card__requester">
                <strong>${escapeHTML(m.requester)}</strong>
                <button class="c-btn c-btn--ghost c-btn--icon map-alias-btn" data-name="${escapeHTML(m.requester)}" data-source="${m.source}" title="Map User">🔗</button>
            </div>
            <div class="c-task-card__assignee">${assigneeText}</div>
            <div class="c-task-card__time">
                <span class="timestamp meta-val">${displayTime}</span>
                ${deadlineBadge}
            </div>
            <div class="c-task-card__actions">
                ${m.has_original ? `<button class="c-btn c-btn--ghost c-btn--icon view-original-btn" title="${i18n.viewOriginal || 'View Original'}">${ICONS.viewOriginal}</button>` : ''}
                <button class="c-btn c-btn--danger c-btn--icon delete-btn" title="${i18n?.delete || (i18n as any)?.deleteBtnText || 'Delete'}">${ICONS.delete}</button>
                <button class="c-btn c-btn--primary c-btn--icon toggle-done-btn">
                    ${m.done ? '↩️' : '✅'}
                </button>
            </div>
        </div>
    `;
}

/**
 * Renders message cards based on data and current state.
 */
export function renderMessages(messages: any[], handlers: any) {
    const currentTab = document.querySelector('.tab-btn.active')?.getAttribute('data-tab') || 'myTasksTab';
    const searchQuery = (document.getElementById('taskSearch') as HTMLInputElement)?.value || '';

    const filtered = sortAndFilterMessages(messages, currentTab, searchQuery);
    const counts = classifyMessages(messages);

    const updateCount = (id: string, count: number) => {
        const el = document.getElementById(id);
        if (el) el.textContent = count.toString();
    };
    updateCount('myCount', (counts as any).my);
    updateCount('otherCount', (counts as any).others);
    updateCount('waitingCount', (counts as any).waiting);
    updateCount('allCount', (counts as any).all);

    const gridId = currentTab.replace('Tab', 'List');
    const grid = document.getElementById(gridId);
    if (!grid) return;

    // Cache handlers on the grid for the centralized delegator in app.ts to use
    (grid as any)._handlers = handlers;

    const isMyTasksDone = (currentTab === 'myTasksTab' && (counts as any).my === 0 && !searchQuery);

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
export function renderArchive(messages: any[]) {
    const tableBody = document.getElementById('archiveBody');
    if (!tableBody) return;

    if (!messages || messages.length === 0) {
        tableBody.innerHTML = '<tr><td colspan="8" class="empty-state">No archived messages</td></tr>';
        return;
    }

    tableBody.innerHTML = messages.map(m => {
        const sourceIcon = m.source === 'slack' ? ICONS.slack : m.source === 'whatsapp' ? ICONS.whatsapp : ICONS.gmail;
        const ts = m.timestamp || m.created_at;
        const compTs = m.completed_at || '-';

        const isMe = m.assignee?.toLowerCase() === 'me';
        const assigneeHtml = isMe
            ? `<span class="c-badge c-badge--accent">${escapeHTML(m.assignee)}</span>`
            : `<span class="c-badge c-badge--dim">${escapeHTML(m.assignee || '-')}</span>`;

        const isDeleted = m.is_deleted === true || m.is_deleted === 1;
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
