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
 * @returns {string} HTML string for the badge.
 */
function getDeadlineBadge(timestamp) {
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
        grid.innerHTML = `
            <div class="empty-state">
                <div class="empty-icon">📂</div>
                <p>${I18N_DATA[state.currentLang].noTasks || 'No tasks found'}</p>
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
    const isConnected = status === STATUS_STATES.CONNECTED ||
        status === STATUS_STATES.AUTHENTICATED ||
        status === true;

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
        const svgSlack = `<svg viewBox="0 0 100 100" width="20" height="20"><path fill="#E01E5A" d="M22.9,53.8V42.3c0-3.2-2.6-5.8-5.8-5.8s-5.8,2.6-5.8,5.8v11.5c0,3.2,2.6,5.8,5.8,5.8S22.9,57,22.9,53.8z"/><path fill="#E01E5A" d="M28.6,42.3c0-3.2,2.6-5.8,5.8-5.8h11.5c3.2,0,5.8,2.6,5.8,5.8s-2.6,5.8-5.8,5.8H34.4C31.2,48.1,28.6,45.5,28.6,42.3z"/><path fill="#36C5F0" d="M46.2,22.9h11.5c3.2,0,5.8-2.6,5.8-5.8s-2.6-5.8-5.8-5.8H46.2c-3.2,0-5.8,2.6-5.8,5.8S43,22.9,46.2,22.9z"/><path fill="#36C5F0" d="M57.7,28.6c3.2,0,5.8,2.6,5.8,5.8v11.5c0,3.2-2.6,5.8-5.8,5.8s-2.6-5.8-5.8-5.8V34.4C51.9,31.2,54.5,28.6,57.7,28.6z"/><path fill="#2EB67D" d="M77.1,46.2v11.5c0,3.2,2.6,5.8,5.8,5.8s5.8-2.6,5.8-5.8V46.2c0-3.2-2.6-5.8-5.8-5.8S77.1,43,77.1,46.2z"/><path fill="#2EB67D" d="M71.4,57.7c0,3.2-2.6,5.8-5.8,5.8H54.1c-3.2,0-5.8-2.6-5.8-5.8s2.6-5.8,5.8-5.8h11.5C68.8,51.9,71.4,54.5,71.4,57.7z"/><path fill="#ECB22E" d="M53.8,77.1H42.3c-3.2,0-5.8,2.6-5.8,5.8s2.6,5.8,5.8,5.8h11.5c3.2,0,5.8-2.6,5.8-5.8S57,77.1,53.8,77.1z"/><path fill="#ECB22E" d="M42.3,71.4c-3.2,0-5.8-2.6-5.8-5.8V54.1c0-3.2,2.6-5.8,5.8-5.8c3.2,0,5.8,2.6,5.8,5.8v11.5C48.1,68.8,45.5,71.4,42.3,71.4z"/></svg>`;
        const svgWA = `<svg viewBox="0 0 448 512" width="20" height="20"><path fill="#25D366" d="M380.9 97.1C339 55.1 283.2 32 223.9 32c-122.4 0-222 99.6-222 222 0 39.1 10.2 77.3 29.6 111L0 480l117.7-30.9c32.4 17.7 68.9 27 106.1 27h.1c122.3 0 224.1-99.6 224.1-222 0-59.3-25.2-115-67.1-157zm-157 341.6c-33.2 0-65.7-8.9-94-25.7l-6.7-4-69.8 18.3L72 359.2l-4.4-7c-18.5-29.4-28.2-63.3-28.2-98.2 0-101.7 82.8-184.5 184.6-184.5 49.3 0 95.6 19.2 130.4 54.1 34.8 34.9 56.2 81.2 56.1 130.5 0 101.8-84.9 184.6-186.6 184.6zm101.2-138.2c-5.5-2.8-32.8-16.2-37.9-18-5.1-1.9-8.8-2.8-12.5 2.8-3.7 5.6-14.3 18-17.6 21.8-3.2 3.7-6.5 4.2-12 1.4-5.5-2.8-23.2-8.5-44.2-27.1-16.4-14.6-27.4-32.7-30.6-38.2-3.2-5.6-.3-8.6 2.4-11.3 2.5-2.4 5.5-6.5 8.3-9.7 2.8-3.3 3.7-5.6 5.6-9.3 1.8-3.7.9-6.9-.5-9.7-1.4-2.8-12.5-30.1-17.1-41.2-4.5-10.8-9.1-9.3-12.5-9.5-3.2-.2-6.9-.2-10.6-.2-3.7 0-9.7 1.4-14.8 6.9-5.1 5.6-19.4 19-19.4 46.3 0 27.3 19.9 53.7 22.6 57.4 2.8 3.7 39.1 59.7 94.8 83.8 13.2 5.7 23.5 9.2 31.6 11.8 13.3 4.2 25.4 3.6 35 2.2 10.7-1.6 32.8-13.4 37.4-26.4 4.6-13 4.6-24.1 3.2-26.4-1.3-2.5-5-3.9-10.5-6.6z"/></svg>`;
        const svgGmail = `<svg viewBox="0 0 512 512" width="20" height="20"><path fill="#EA4335" d="M48 64C21.5 64 0 85.5 0 112v288c0 26.5 21.5 48 48 48h416c26.5 0 48-21.5 48-48V112c0-26.5-21.5-48-48-48H48zM48 96h416c8.8 0 16 7.2 16 16v21.3L256 295.1 32 133.3V112c0-8.8 7.2-16 16-16zm-16 70.6l208 147.3L448 166.6V400c0 8.8-7.2 16-16 16H80c-8.8 0-16-7.2-16-16V166.6z"/></svg>`;

        const lang = state.currentLang;
        const i18n = I18N_DATA[lang];
        const ts = m.timestamp || m.created_at;
        const displayTime = formatDisplayTime(ts, lang);
        const deadlineBadge = getDeadlineBadge(ts);

        const sourceIcon = m.source === 'slack' ? svgSlack : m.source === 'whatsapp' ? svgWA : svgGmail;
        const assigneeText = m.assignee === 'me' ? `<span class="assignee-me">${i18n.assigneeMe}</span>` : `<span class="assignee-other">${m.assignee}</span>`;

        return `
            <div class="card ${m.source} ${m.done ? 'done' : ''}" id="task-${m.id}" data-id="${m.id}">
                <div class="col-source" title="${m.source.toUpperCase()}">
                    ${sourceIcon}
                </div>
                <div class="col-room">${m.room ? `<span class="badge-room">${escapeHTML(m.room)}</span>` : '-'}</div>
                <div class="col-task">
                    <span class="task-title">${escapeHTML(m.task)}</span>
                    ${m.category === 'waiting' ? `<div class="waiting-tag" style="font-size: 0.75rem; color: var(--accent-color);">⏳ Waiting...</div>` : ''}
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
        const status = (statusStr || '').toLowerCase();
        const isConnected = status === STATUS_STATES.CONNECTED.toLowerCase() || status === STATUS_STATES.AUTHENTICATED.toLowerCase();

        console.log(`[DEBUG] WA Status Check: raw=${statusStr}, processed=${status}, isConnected=${isConnected}`);

        const largeIcon = document.getElementById(DOM_IDS.WHATSAPP_DOT);
        const textLabel = document.getElementById(DOM_IDS.WHATSAPP_TEXT);

        if (largeIcon) {
            largeIcon.classList.toggle('active', isConnected);
            largeIcon.classList.toggle('inactive', !isConnected);
        }
        if (textLabel) {
            textLabel.textContent = isConnected ? UI_TEXT.ON : UI_TEXT.OFF;
        }
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
            const svgSlack = `<svg viewBox="0 0 100 100" width="16" height="16"><path fill="#E01E5A" d="M22.9,53.8V42.3c0-3.2-2.6-5.8-5.8-5.8s-5.8,2.6-5.8,5.8v11.5c0,3.2,2.6,5.8,5.8,5.8S22.9,57,22.9,53.8z"/><path fill="#E01E5A" d="M28.6,42.3c0-3.2,2.6-5.8,5.8-5.8h11.5c3.2,0,5.8,2.6,5.8,5.8s-2.6,5.8-5.8,5.8H34.4C31.2,48.1,28.6,45.5,28.6,42.3z"/><path fill="#36C5F0" d="M46.2,22.9h11.5c3.2,0,5.8-2.6,5.8-5.8s-2.6-5.8-5.8-5.8H46.2c-3.2,0-5.8,2.6-5.8,5.8S43,22.9,46.2,22.9z"/><path fill="#36C5F0" d="M57.7,28.6c3.2,0,5.8,2.6,5.8,5.8v11.5c0,3.2-2.6,5.8-5.8,5.8s-2.6-5.8-5.8-5.8V34.4C51.9,31.2,54.5,28.6,57.7,28.6z"/><path fill="#2EB67D" d="M77.1,46.2v11.5c0,3.2,2.6,5.8,5.8,5.8s5.8-2.6,5.8-5.8V46.2c0-3.2-2.6-5.8-5.8-5.8S77.1,43,77.1,46.2z"/><path fill="#2EB67D" d="M71.4,57.7c0,3.2-2.6,5.8-5.8,5.8H54.1c-3.2,0-5.8-2.6-5.8-5.8s2.6-5.8,5.8-5.8h11.5C68.8,51.9,71.4,54.5,71.4,57.7z"/><path fill="#ECB22E" d="M53.8,77.1H42.3c-3.2,0-5.8,2.6-5.8,5.8s2.6,5.8,5.8,5.8h11.5c3.2,0,5.8-2.6,5.8-5.8S57,77.1,53.8,77.1z"/><path fill="#ECB22E" d="M42.3,71.4c-3.2,0-5.8-2.6-5.8-5.8V54.1c0-3.2,2.6-5.8,5.8-5.8c3.2,0,5.8,2.6,5.8,5.8v11.5C48.1,68.8,45.5,71.4,42.3,71.4z"/></svg>`;
            const svgWA = `<svg viewBox="0 0 448 512" width="16" height="16"><path fill="#25D366" d="M380.9 97.1C339 55.1 283.2 32 223.9 32c-122.4 0-222 99.6-222 222 0 39.1 10.2 77.3 29.6 111L0 480l117.7-30.9c32.4 17.7 68.9 27 106.1 27h.1c122.3 0 224.1-99.6 224.1-222 0-59.3-25.2-115-67.1-157zm-157 341.6c-33.2 0-65.7-8.9-94-25.7l-6.7-4-69.8 18.3L72 359.2l-4.4-7c-18.5-29.4-28.2-63.3-28.2-98.2 0-101.7 82.8-184.5 184.6-184.5 49.3 0 95.6 19.2 130.4 54.1 34.8 34.9 56.2 81.2 56.1 130.5 0 101.8-84.9 184.6-186.6 184.6zm101.2-138.2c-5.5-2.8-32.8-16.2-37.9-18-5.1-1.9-8.8-2.8-12.5 2.8-3.7 5.6-14.3 18-17.6 21.8-3.2 3.7-6.5 4.2-12 1.4-5.5-2.8-23.2-8.5-44.2-27.1-16.4-14.6-27.4-32.7-30.6-38.2-3.2-5.6-.3-8.6 2.4-11.3 2.5-2.4 5.5-6.5 8.3-9.7 2.8-3.3 3.7-5.6 5.6-9.3 1.8-3.7.9-6.9-.5-9.7-1.4-2.8-12.5-30.1-17.1-41.2-4.5-10.8-9.1-9.3-12.5-9.5-3.2-.2-6.9-.2-10.6-.2-3.7 0-9.7 1.4-14.8 6.9-5.1 5.6-19.4 19-19.4 46.3 0 27.3 19.9 53.7 22.6 57.4 2.8 3.7 39.1 59.7 94.8 83.8 13.2 5.7 23.5 9.2 31.6 11.8 13.3 4.2 25.4 3.6 35 2.2 10.7-1.6 32.8-13.4 37.4-26.4 4.6-13 4.6-24.1 3.2-26.4-1.3-2.5-5-3.9-10.5-6.6z"/></svg>`;
            const svgGmail = `<svg viewBox="0 0 512 512" width="16" height="16"><path fill="#EA4335" d="M48 64C21.5 64 0 85.5 0 112v288c0 26.5 21.5 48 48 48h416c26.5 0 48-21.5 48-48V112c0-26.5-21.5-48-48-48H48zM48 96h416c8.8 0 16 7.2 16 16v21.3L256 295.1 32 133.3V112c0-8.8 7.2-16 16-16zm-16 70.6l208 147.3L448 166.6V400c0 8.8-7.2 16-16 16H80c-8.8 0-16-7.2-16-16V166.6z"/></svg>`;
            const sourceIcon = m.source === 'slack' ? svgSlack : m.source === 'whatsapp' ? svgWA : svgGmail;
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
