import { escapeHTML, TimeService } from '../utils';
import { ICONS } from '../icons';
import { getDeadlineBadge, getDisplayTask } from '../logic';
import { I18N_DATA } from '../locales';
import { I18nDictionary, Message } from '../types';
import { renderSourceList } from '../renderers/task-renderer';
import { ASSIGNEE_SHARED } from '../constants';

export type MessageCardProps = Message & {
    lang: string;
    isSelected?: boolean;
};

/**
 * Why: Safely parses metadata from either string or object format.
 */
function parseMetadata(metadata: any): Record<string, any> | null {
    if (!metadata) return null;
    if (typeof metadata === 'object') return metadata;
    try {
        return JSON.parse(metadata);
    } catch (e) {
        return null;
    }
}

/**
 * Why: Implementation of a Pure Component for message rendering that strictly adheres to BEM and rem units.
 * Decouples rendering logic from the main application state to allow for independent testing.
 */
export function MessageCard(props: MessageCardProps): string {
    const { id, source, source_channels, room, is_translating, requester, assignee, timestamp, created_at, done, category, metadata: rawMetadata, lang, translating: oldTranslating, translationError, has_original, assigned_to, isSelected } = props;
    
    // Unified translating state (support legacy and new fields)
    const translating = oldTranslating || is_translating;
    const displayTask = getDisplayTask(props, lang);
    
    // Why: Ensure time is extracted from either timestamp or created_at.
    const rawTime = String(timestamp || created_at || "");
    const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
    const displayTime = TimeService.formatDisplayTime(rawTime, lang);
    const deadlineBadge = getDeadlineBadge(rawTime, done, lang);

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

    const categoryBadgeHtml = category === 'POLICY' ? `<div class="c-message-card__badge c-message-card__badge--policy">${i18n.policyLabel || 'Policy'}</div>` : 
                             category === 'QUERY' ? `<div class="c-message-card__badge c-message-card__badge--query">${i18n.queryLabel || 'Question'}</div>` :
                             category === 'promise' ? `<div class="c-message-card__badge c-message-card__badge--promise">🤝 ${i18n.promise || '약속'}</div>` :
                             category === 'waiting' ? `<div class="c-message-card__badge c-message-card__badge--waiting">⏳ ${i18n.waiting || '대기'}</div>` : '';

    const translatingBadgeHtml = translating ? `<span class="c-message-card__translating-badge" title="Translating...">⏳</span>` : '';

    const delegatedHtml = assigned_to ? `<div class="c-message-card__badge c-message-card__badge--delegated" title="Delegated Task">🔄 ${lang === 'ko' ? `@${escapeHTML(assigned_to)}에게 위임됨` : `Delegated to @${escapeHTML(assigned_to)}`}</div>` : '';

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

    const assigneeMe = i18n?.assigneeMe || 'Me';
    const isMe = assignee === 'me';
    const isShared = assignee === ASSIGNEE_SHARED;
    const isInvalid = !assignee || assignee === 'undefined' || assignee === 'unknown';
    
    let assigneeHtml = '';
    if (isMe) {
        assigneeHtml = `<span class="c-message-card__assignee--me">${assigneeMe}</span>`;
    } else if (isShared) {
        assigneeHtml = `<span class="c-message-card__assignee--shared">${i18n.assigneeShared || 'Shared'}</span>`;
    } else {
        assigneeHtml = `<span class="c-message-card__assignee--other">${isInvalid ? '-' : escapeHTML(assignee)}</span>`;
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
                    ${translationError ? `<span class="c-message-card__error-hint" title="${escapeHTML(translationError)}">⚠️</span>` : ''}
                    ${translatingBadgeHtml}
                    ${escapeHTML(displayTask)}
                </div>
                ${constraintsHtml}
            </div>

            <div class="c-message-card__footer">
                <div class="c-message-card__info-group">
                    <div class="c-message-card__requester" title="Requester">
                        <span class="c-message-card__label">👤</span>
                        <strong class="c-message-card__name">${escapeHTML(requester)}</strong>
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
