import { state, updateStats } from './state.js';
import { I18N_DATA } from './locales.js';
import { formatDisplayTime, escapeHTML } from './utils.js';

import { sortAndFilterMessages, classifyMessages, calculateHeatmapLevel, calculateSourceDistribution } from './logic.js';
import { DOM_IDS, STATUS_STATES, UI_TEXT } from './constants.js';
import { ICONS } from './icons.js';

/**
 * @file renderer.js
 * @description UI rendering module for tasks and user profile.
 */

/**
 * Gets the deadline badge HTML based on the task timestamp.
 * @param {string} timestamp - ISO timestamp string.
 * @param {boolean} isDone - Whether the task is completed.
 * @returns {string} HTML string for the badge.
 */
function getDeadlineBadge(timestamp, isDone) {
    if (isDone) return '';
    const diffHours = (new Date() - new Date(timestamp)) / (1000 * 60 * 60);
    const lang = state.currentLang || 'ko';

    if (diffHours >= 72) {
        return `<span class="badge badge-abandoned" style="margin-left: 0.3rem; display: inline-flex; align-items: center; gap: 4px;">${ICONS.abandoned}${I18N_DATA[lang].abandoned}</span>`;
    }
    if (diffHours >= 24) {
        return `<span class="badge badge-stale" style="margin-left: 0.3rem; display: inline-flex; align-items: center; gap: 4px;">${ICONS.stale}${I18N_DATA[lang].stale}</span>`;
    }
    return '';
}

/**
 * Attaches event listeners for alias mapping interactions.
 */
// This function is now imported from modals.js, so it's removed from here.

/**
 * Renders an empty grid state when no tasks are found.
 */
function renderEmptyGrid(grid) {
    if (grid) {
        const lang = state.currentLang || 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        let displayMsg = I18N_DATA[lang].noTasks || 'No tasks found';

        if (messages && messages.length > 0) {
            const randomIndex = Math.floor(Math.random() * messages.length);
            displayMsg = messages[randomIndex];
        }

        grid.innerHTML = `
            <div class="empty-state-witty">
                <div class="empty-icon" style="font-size: 3rem; margin-bottom: 1rem;">✨</div>
                <div class="witty-message">${displayMsg}</div>
            </div>
        `;
    }
}

/**
 * Common utility to update service status in the UI dashboard and settings.
 * @param {string} service - Service name (slack, wa, gmail).
 * @param {string|boolean} status - Connection status.
 */
function updateServiceStatusUI(service, status) {
    let isConnected = status === true;
    if (typeof status === 'string') {
        const normalized = status.toLowerCase();
        isConnected = normalized === STATUS_STATES.CONNECTED.toLowerCase() || normalized === STATUS_STATES.AUTHENTICATED.toLowerCase();
    }

    // 1. Large status icon/label in dashboard
    const largeIcon = document.getElementById(DOM_IDS.STATUS_LARGE(service));
    const largeLabel = document.getElementById(DOM_IDS.STATUS_TEXT(service));

    if (largeIcon) {
        largeIcon.classList.toggle('active', isConnected);
        largeIcon.classList.toggle('inactive', !isConnected);
    }
    if (largeLabel) {
        largeLabel.textContent = isConnected ? UI_TEXT.ON : UI_TEXT.OFF;
    }

    // 2. Settings menu status pills (if any)
    const settingsPill = document.getElementById(`${service}ConnectedStatus`);
    if (settingsPill) {
        settingsPill.classList.toggle('hidden', !isConnected);
    }
}

export const renderer = {
    /**
     * Renders message cards based on data and current state.
     * @param {import('./logic.js').Message[]} messages - Array of messages.
     * @param {Object} handlers - Event handlers for actions.
     */
    renderMessages(messages, handlers) {
        const currentTab = document.querySelector('.tab-btn.active')?.getAttribute('data-tab') || 'myTasksTab';
        const searchQuery = document.getElementById('taskSearch')?.value || '';

        // [DEBUG] renderMessages: rawCount=${messages.length}, currentTab=${currentTab}
        const filtered = sortAndFilterMessages(messages, currentTab, searchQuery);
        const counts = classifyMessages(messages);
        console.log(`[DEBUG] renderMessages: filteredCount=${filtered.length}, counts=`, counts);

        // Update counts in UI
        const updateCount = (id, count) => {
            const el = document.getElementById(id);
            if (el) el.textContent = count;
        };
        updateCount('myCount', counts.my);
        updateCount('otherCount', counts.others);
        updateCount('waitingCount', counts.waiting);
        updateCount('allCount', counts.all);

        // 활성화된 탭 이름(예: allTasksTab)을 기반으로 리스트 컨테이너(allTasksList) 찾기
        const gridId = currentTab.replace('Tab', 'List');
        const grid = document.getElementById(gridId);
        if (!grid) return;

        if (filtered.length === 0) {
            renderEmptyGrid(grid);
            return;
        }

        grid.innerHTML = filtered.map(m => this.createCardElement(m)).join('');
        this.attachCardEventListeners(filtered, handlers);

    },

    /**
     * Creates HTML string for a single message card.
     * @param {import('./logic.js').Message} m - Message object.
     * @returns {string} HTML string.
     */
    createCardElement(m) {

        const lang = state.currentLang;
        const i18n = I18N_DATA[lang];
        const ts = m.timestamp || m.created_at;
        const displayTime = formatDisplayTime(ts, lang);
        const deadlineBadge = getDeadlineBadge(ts, m.done);

        const sourceIcon = m.source === 'slack' ? ICONS.slack : m.source === 'whatsapp' ? ICONS.whatsapp : ICONS.gmail;
        const assigneeText = m.assignee === 'me' ? `<span class="assignee-me">${i18n.assigneeMe}</span>` : `<span class="assignee-other">${m.assignee}</span>`;

        return `
            <div class="card ${m.source} ${m.done ? 'done' : ''}" id="task-${m.id}" data-id="${m.id}">
                <div class="col-source" title="${m.source.toUpperCase()}">
                    ${sourceIcon}
                </div>
                <div class="col-room">${m.room ? `<span class="badge-room">${escapeHTML(m.room)}</span>` : '-'}</div>
                <div class="col-task">
                    <span class="task-title">${escapeHTML(m.task)}</span>
                    ${m.category === 'waiting' && !m.done ? `<div class="waiting-tag" style="font-size: 0.75rem; color: var(--accent-color);">⏳ Waiting...</div>` : ''}
                </div>
                <div class="col-requester">
                    <strong>${escapeHTML(m.requester)}</strong>
                    <button class="map-alias-btn" data-name="${escapeHTML(m.requester)}" data-source="${m.source}" title="Map User" style="background:none;border:none;cursor:pointer;padding:0;font-size:0.9rem;">🔗</button>
                </div>
                <div class="col-assignee">${assigneeText}</div>
                <div class="col-time">
                    <span class="timestamp meta-val">${displayTime}</span>
                    ${deadlineBadge}
                </div>
                <div class="col-actions">
                    <button class="action-btn original-btn show-original" title="View Original">${ICONS.viewOriginal}</button>
                    <button class="action-btn delete-btn delete-task" title="${i18n.delete}">${ICONS.delete}</button>
                    <button class="done-btn toggle-done">
                        ${m.done ? '↩️' : '✅'}
                    </button>
                </div>
            </div>
        `;
    },

    /**
     * Attaches event listeners to message cards.
     * @param {import('./logic.js').Message[]} messages - Array of messages.
     * @param {Object} handlers - Event handlers.
     */
    attachCardEventListeners(messages, handlers) {
        document.querySelectorAll('.card').forEach(card => {
            const id = parseInt(card.getAttribute('data-id'));
            const m = messages.find(item => item.id === id);

            card.querySelector('.toggle-done')?.addEventListener('click', () => {
                handlers.onToggleDone(id, !m.done);
            });

            card.querySelector('.delete-task')?.addEventListener('click', () => {
                if (confirm(I18N_DATA[state.currentLang].confirmDelete)) {
                    handlers.onDeleteTask(id);
                }
            });

            // [Refactored] Removed window.showOriginalMessage
            card.querySelector('.show-original')?.addEventListener('click', () => {
                if (handlers.onShowOriginal) handlers.onShowOriginal(id);
            });
        });
    },

    /**
     * Triggers XP animation in the UI.
     */
    triggerXPAnimation() {
        const overlay = document.getElementById('xpOverlay');
        if (!overlay) return;
        overlay.classList.remove('hidden');
        overlay.style.animation = 'none';
        overlay.offsetHeight; // trigger reflow
        overlay.style.animation = 'xpFloat 1.2s ease-out forwards';
        setTimeout(() => overlay.classList.add('hidden'), 1200);
    },

    /**
     * Triggers confetti animation.
     */
    triggerConfetti() {
        if (typeof confetti === 'function') {
            confetti({
                particleCount: 100,
                spread: 70,
                origin: { y: 0.6 },
                colors: ['#00d4ff', '#0052ff', '#ffffff']
            });
        }
    },

    /**
     * Updates user profile UI with latest data.
     * @param {Object} profile - User profile data.
     */
    updateUserProfile(profile) {
        if (!profile) return;

        // Unhide profile sections
        const userProfile = document.getElementById('userProfile');
        const gamificationStats = document.getElementById('gamificationStats');
        if (userProfile) userProfile.classList.remove('hidden');
        if (gamificationStats) gamificationStats.classList.remove('hidden');

        // Update basic info
        const userEmail = document.getElementById('userEmail');
        const userPic = document.getElementById('userPicture');
        if (userEmail) userEmail.textContent = profile.email || '';
        if (userPic && profile.picture) {
            userPic.src = profile.picture;
            userPic.classList.remove('hidden');
        }

        const locale = I18N_DATA[state.currentLang];
        const streakText = document.getElementById('userStreak');
        const xpText = document.getElementById('xpText');
        const xpBar = document.getElementById('xpBar');
        const pointsText = document.getElementById('userPoints');
        const levelText = document.getElementById('userLevel');

        if (streakText) streakText.textContent = `${profile.streak || 0}🔥`;
        if (xpText) xpText.textContent = `${profile.xp || 0} / 100 XP`;
        if (xpBar) {
            const progress = (profile.xp || 0) % 100;
            xpBar.style.width = `${progress}%`;
        }
        if (pointsText) pointsText.textContent = profile.points || 0;
        if (levelText) levelText.textContent = profile.level || 1;

        // Updates for Streak Freezes
        const freezeContainer = document.getElementById('streakFreezeContainer');
        if (freezeContainer) {
            const count = profile.streak_freezes || 0;
            let html = `<span class="freeze-badge" title="Streak Freeze">❄️ × ${count}</span>`;

            if (profile.points >= 50) {
                html += `<button class="buy-freeze-btn" id="buyFreezeBtn">+ ❄️ (50 SCORE)</button>`;
            }
            freezeContainer.innerHTML = html;
        }

    },

    /**
     * Updates Slack connection status.
     * @param {boolean|string} status - Status.
     */
    updateSlackStatus(status) {
        updateServiceStatusUI('slack', status);
    },

    /**
     * @param {Object} data - Connection status data, e.g., { status: 'CONNECTED' }
     */
    updateWhatsAppStatus: (statusStr) => {
        updateServiceStatusUI('wa', statusStr);
    },

    /**
     * Updates Gmail connection status.
     * @param {boolean} connected - Is connected.
     */
    updateGmailStatus(connected) {
        updateServiceStatusUI('gmail', connected);
    },

    /**
     * Updates the token usage badge in the header.
     * @param {Object} usage - Token usage object.
     */
    updateTokenBadge(usage) {
        const badge = document.getElementById('tokenUsageBadge');
        if (badge && usage) {
            badge.textContent = `Token: ${usage.todayTotal || 0}`;
            // Optional: trigger subtle pop animation
            badge.style.transform = 'scale(1.1)';
            setTimeout(() => badge.style.transform = 'scale(1)', 200);
        }
    },

    /**
     * Renders archived messages in the archive table.
     * @param {import('./logic.js').Message[]} messages - Array of archived messages.
     */
    renderArchive(messages) {
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

            return `
                <tr>
                    <td><input type="checkbox" class="archive-check" data-id="${m.id}"></td>
                    <td title="${m.source.toUpperCase()}" style="display:flex;align-items:center;height:100%;">${sourceIcon}</td>
                    <td>${m.room ? `<span class="room-badge">${escapeHTML(m.room)}</span>` : '-'}</td>
                    <td>${escapeHTML(m.task)}</td>
                    <td>${escapeHTML(m.requester)}</td>
                    <td>${escapeHTML(m.assignee)}</td>
                    <td>${formatDisplayTime(ts, state.currentLang)}</td>
                    <td>${compTs !== '-' ? formatDisplayTime(compTs, state.currentLang) : '-'}</td>
                </tr>
            `;
        }).join('');
    },

    /**
     * Renders the list of user aliases in settings.
     * @param {string[]} aliases - List of aliases.
     * @param {Function} onRemove - Callback function when an alias is removed.
     */
    renderAliasList(aliases, onRemove) {
        const container = document.getElementById('aliasList');
        if (!container) return;

        if (!aliases || aliases.length === 0) {
            container.innerHTML = '<p class="empty-list">No aliases configured</p>';
            return;
        }

        container.innerHTML = aliases.map(alias => `
            <div class="alias-item">
                <span>${escapeHTML(alias)}</span>
                <button class="remove-alias-btn" data-alias="${escapeHTML(alias)}">&times;</button>
            </div>
        `).join('');

        container.querySelectorAll('.remove-alias-btn').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.dataset.alias));
        });
    },

    /**
     * Renders the list of tenant aliases in settings.
     * @param {string[]} aliases - List of tenant aliases.
     * @param {Function} onRemove - Callback function when an alias is removed.
     */
    renderTenantAliasList(aliases, onRemove) {
        const container = document.getElementById('normList');
        if (!container) return;

        if (!aliases || aliases.length === 0) {
            container.innerHTML = '<p class="empty-list">No tenant aliases configured</p>';
            return;
        }

        container.innerHTML = aliases.map(alias => `
            <div class="alias-item">
                <span>${escapeHTML(alias)}</span>
                <button class="remove-tenant-alias-btn" data-alias="${escapeHTML(alias)}">&times;</button>
            </div>
        `).join('');

        container.querySelectorAll('.remove-tenant-alias-btn').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.dataset.alias));
        });
    },

    /**
     * Renders contact mappings in settings.
     * @param {Object[]} mappings - List of mappings.
     * @param {Function} onRemove - Callback function when a mapping is removed.
     */
    renderContactMappings(mappings, onRemove) {
        const container = document.getElementById('contactList');
        if (!container) return;

        if (!mappings || mappings.length === 0) {
            container.innerHTML = '<p class="empty-list">No contact mappings</p>';
            return;
        }

        container.innerHTML = mappings.map(m => `
            <div class="mapping-item">
                <span class="mapping-source">${m.source}: ${escapeHTML(m.name)}</span>
                <span class="mapping-arrow">→</span>
                <span class="mapping-target">${escapeHTML(m.alias)}</span>
                <button class="remove-mapping-btn" data-id="${m.id}">&times;</button>
            </div>
        `).join('');

        container.querySelectorAll('.remove-mapping-btn').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.dataset.id));
        });
    },

    /**
     * Renders release notes in the modal.
     * @param {string} content - Markdown content of release notes.
     */
    renderReleaseNotes(content) {
        const container = document.getElementById('releaseNotesContent');
        if (!container) return;

        // 유실되었던 '전용 마크다운-HTML 엔진' 복원 및 스타일 강화
        const parseMarkdown = (text) => {
            if (!text) return '';
            return text
                .replace(/^### (.*$)/gim, '<h3 style="margin-top: 1.5rem; margin-bottom: 0.5rem; color: var(--text-main);">$1</h3>')
                .replace(/^## (.*$)/gim, '<h2 style="margin-top: 1.8rem; margin-bottom: 0.8rem; color: var(--text-main); border-bottom: 1px solid rgba(255,255,255,0.1); padding-bottom: 0.3rem;">$1</h2>')
                .replace(/^# (.*$)/gim, '<h1 style="margin-top: 2rem; margin-bottom: 1rem; color: var(--accent-color); font-size: 1.4rem;">$1</h1>')
                .replace(/^\-\-\-/gim, '<hr class="settings-divider" style="margin: 2rem 0;">')
                .replace(/\*\*(.*?)\*\*/gim, '<strong style="color: var(--text-main); font-weight: 800;">$1</strong>')
                .replace(/`(.*?)`/gim, '<code style="background: rgba(255,255,255,0.1); padding: 0.2rem 0.4rem; border-radius: 4px; font-size: 0.9em; font-family: monospace;">$1</code>')
                .replace(/^\- (.*$)/gim, '<div style="padding-left: 1rem; text-indent: -0.8rem; margin-bottom: 0.5rem; color: var(--text-dim); line-height: 1.6;"><span style="color: var(--accent-color);">•</span> $1</div>')
                .replace(/\n/gim, '<br>')
                .replace(/(<\/h[1-3]>|<hr.*?>|<\/div>)<br>/gim, '$1'); // 블록 요소 뒤 불필요한 줄바꿈 정리
        };

        container.innerHTML = `<div class="release-notes-markdown">${parseMarkdown(content)}</div>`;
    }
};
