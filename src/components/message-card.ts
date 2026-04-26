import { escapeHTML, TimeService } from '../utils';
import { ICONS } from '../icons';
import { getDeadlineBadge, getDisplayTask } from '../logic';
import { I18N_DATA } from '../locales';
import { I18nDictionary, Message } from '../types';
import { renderSourceList } from '../renderers/task-renderer';
import { ASSIGNEE_SHARED } from '../constants';
import { parseTaskContext } from '../logic/task-context';

export type MessageCardProps = Message & {
    lang: string;
    isSelected?: boolean;
    currentUserNames?: string[];
};

/**
 * Why: Safely parses metadata from either string or object format.
 * Returns Record<string, unknown> — readers must narrow individual fields before use.
 */
function parseMetadata(metadata: unknown): Record<string, unknown> | null {
    if (!metadata) return null;
    if (typeof metadata === 'object') return metadata as Record<string, unknown>;
    if (typeof metadata !== 'string') return null;
    try {
        const parsed: unknown = JSON.parse(metadata);
        if (parsed && typeof parsed === 'object') return parsed as Record<string, unknown>;
        return null;
    } catch {
        return null;
    }
}

/**
 * Why: Implementation of a Pure Component for message rendering that strictly adheres to BEM and rem units.
 * Decouples rendering logic from the main application state to allow for independent testing.
 */
export function MessageCard(props: MessageCardProps): string {
    const { id, source, source_channels, room, is_translating, requester, assignee, timestamp, created_at, done, category, metadata: rawMetadata, lang, translation_error, has_original, assigned_to, subtasks, isSelected, currentUserNames, deadline } = props;

    const isSelf = (name: string | undefined): boolean =>
        !!name && !!currentUserNames?.length &&
        currentUserNames.some(n => n.toLowerCase() === name.toLowerCase());

    const selfTag = `<span class="c-message-card__self-tag" title="나">✦</span>`;

    // Unified translating state (support legacy and new fields)
    const translating = is_translating;
    const displayTask = getDisplayTask(props, lang);
    
    // Why: Ensure time is extracted from either timestamp or created_at.
    const rawTime = String(timestamp || created_at || "");
    const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
    const displayTime = TimeService.formatDisplayTime(rawTime, lang);
    const deadlineBadge = getDeadlineBadge(deadline, done, lang);
    const contextSnippets = parseTaskContext(props.consolidated_context);

    const metadata = parseMetadata(rawMetadata);
    const isContextQuery = !!metadata?.is_context_query;
    const constraints = Array.isArray(metadata?.constraints) ? metadata.constraints : [];

    const modifierClass = [
        done ? 'c-message-card--done' : '',
        translating ? 'c-message-card--loading' : '',
        category === 'POLICY' ? 'c-message-card--policy' : '',
        category === 'QUERY' ? 'c-message-card--query' : '',
        isContextQuery ? 'c-message-card--context' : '',
        assignee === ASSIGNEE_SHARED ? 'c-message-card--shared' : ''
    ].filter(Boolean).join(' ');

    const isShared = assignee === ASSIGNEE_SHARED || category === 'shared';

    const categoryBadgeHtml = category === 'POLICY' ? `<div class="c-message-card__badge c-message-card__badge--policy">${i18n.policyLabel || 'Policy'}</div>` : 
                             category === 'QUERY' ? `<div class="c-message-card__badge c-message-card__badge--query">${i18n.queryLabel || 'Question'}</div>` :
                             category === 'promise' ? `<div class="c-message-card__badge c-message-card__badge--promise">🤝 ${i18n.promise || '약속'}</div>` :
                             isShared ? `<div class="c-message-card__badge c-message-card__badge--shared">👥 ${i18n.sharedTag || 'Shared'}</div>` : '';

    const translatingBadgeHtml = translating ? `<span class="c-message-card__translating-badge" title="Translating...">⏳</span>` : '';

    const delegatedHtml = assigned_to ? `<div class="c-message-card__badge c-message-card__badge--delegated" title="Delegated Task">🔄 ${lang === 'ko' ? `@${escapeHTML(assigned_to)}에게 위임됨` : `Delegated to @${escapeHTML(assigned_to)}`}</div>` : '';

    const contextHtml = contextSnippets.length > 0 ? `
        <div class="task-context">
            ${ICONS.info}
            <div class="task-context__tooltip">
                <span class="task-context__title">Context Snippet</span>
                ${contextSnippets.map(s => `<div class="task-context__snippet">${escapeHTML(s)}</div>`).join('')}
            </div>
        </div>
    ` : '';

    const loadingOverlay = translating ? `
        <div class="c-message-card__loading-overlay">
            <div class="c-spinner c-spinner--sm"></div>
        </div>
    ` : '';

    const constraintsHtml = (category === 'POLICY' && constraints.length > 0) ? `
        <ul class="c-message-card__constraints">
            ${constraints.map(c => `<li class="c-message-card__constraint-item">${escapeHTML(c)}</li>`).join('')}
        </ul>
    ` : '';

    const doneCount = subtasks ? subtasks.filter(s => s.done).length : 0;
    const subtasksHtml = (subtasks && subtasks.length > 0) ? `
        <details class="c-message-card__subtasks-wrap">
            <summary class="c-message-card__subtasks-toggle">
                <span class="c-message-card__subtasks-label">
                    • ${subtasks.length}${lang === 'ko' ? '개 세부 업무' : ' subtasks'}
                    ${doneCount > 0 ? `<span class="c-message-card__subtasks-progress">(${doneCount}/${subtasks.length})</span>` : ''}
                </span>
            </summary>
            <ul class="c-message-card__subtasks">
                ${subtasks.map((s, idx) => `
                    <li class="c-message-card__subtask-item ${s.done ? 'c-message-card__subtask-item--done' : ''}"
                        data-action="toggle-subtask"
                        data-index="${idx}"
                        role="button"
                        tabindex="0">
                        <span class="c-message-card__subtask-check">${s.done ? '✅' : '•'}</span>
                        <span class="c-message-card__subtask-task">${escapeHTML(s.task)}</span>
                        ${s.assignee ? `<span class="c-message-card__subtask-assignee">${escapeHTML(s.assignee)}</span>` : ''}
                    </li>
                `).join('')}
            </ul>
        </details>
    ` : '';

    const isInvalid = !assignee || assignee === 'undefined' || assignee === 'unknown';

    let assigneeHtml = '';
    if (isShared) {
        assigneeHtml = `<span class="c-message-card__assignee--shared">${i18n.sharedTag || 'Shared'}</span>`;
    } else {
        const assigneeName = isInvalid ? '-' : escapeHTML(assignee);
        const assigneeSelf = !isInvalid && isSelf(assignee) ? selfTag : '';
        assigneeHtml = `<span class="c-message-card__assignee--other">${assigneeName}${assigneeSelf}</span>`;
    }

    return `
        <div class="c-message-card ${modifierClass}" id="task-${id}" data-id="${id}">
            ${loadingOverlay}
            <div class="c-message-card__header">
                <div class="c-message-card__checkbox-wrapper">
                    <input type="checkbox" class="c-message-card__checkbox" data-action="select-task" data-id="${id}" ${isSelected ? 'checked' : ''}>
                </div>
                ${renderSourceList(source_channels, source)}
                <div class="c-message-card__room">${room ? `<span class="c-message-card__badge-room">${escapeHTML(room)}</span>` : '-'}</div>
                ${delegatedHtml}
                ${categoryBadgeHtml}
                <div class="c-message-card__actions">
                    ${has_original ? `<button class="c-message-card__action-btn view-original-btn" data-action="show-original" title="${i18n.viewOriginal || 'View Original'}">${ICONS.viewOriginal}</button>` : ''}
                    <button class="c-message-card__action-btn delete-btn" data-action="delete" title="${i18n?.delete || 'Delete'}">${ICONS.delete}</button>
                    <button class="c-message-card__action-btn c-message-card__action-btn--primary toggle-done-btn" data-action="toggle-done">
                        ${done ? '↩️' : '✅'}
                    </button>
                </div>
            </div>

            <div class="c-message-card__body">
                <div class="c-message-card__title">
                    ${translation_error ? `<span class="c-message-card__error-hint" title="${escapeHTML(translation_error)}">⚠️</span>` : ''}
                    ${translatingBadgeHtml}
                    ${escapeHTML(displayTask)}
                    ${contextHtml}
                </div>
                ${constraintsHtml}
                ${subtasksHtml}
            </div>

            <div class="c-message-card__footer">
                <div class="c-message-card__info-group">
                    <div class="c-message-card__requester" title="Requester">
                        <span class="c-message-card__label">👤</span>
                        <strong class="c-message-card__name">${escapeHTML(requester)}${isSelf(requester) ? selfTag : ''}</strong>
                        <button class="c-message-card__inline-action map-alias-btn"
                                data-action="map-alias" 
                                data-name="${escapeHTML(requester)}" 
                                data-source="${source}" 
                                title="Map User">🔗</button>
                    </div>
                    <div class="c-message-card__assignee" title="Assignee">
                        <span class="c-message-card__label">🛠️</span>
                        ${assigneeHtml}
                    </div>
                </div>
                
                <div class="c-message-card__time-group">
                    <div class="c-message-card__timestamp">${displayTime}</div>
                    ${deadlineBadge ? `<div class="c-message-card__deadline">${deadlineBadge}</div>` : ''}
                </div>
            </div>
        </div>
    `;
}
