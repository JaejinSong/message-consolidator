// v.2.1.982
import { state, updateStats } from './state.js';
import { I18N_DATA } from './locales.js';
import { TimeService, escapeHTML } from './utils.js';

import { sortAndFilterMessages, classifyMessages, calculateHeatmapLevel, calculateSourceDistribution, getDeadlineBadge, parseMarkdown } from './logic.js';
import { DOM_IDS, STATUS_STATES, UI_TEXT } from './constants.js';
import { ICONS } from './icons.js';

/**
 * @file renderer.js
 * @description UI rendering module for tasks and user profile.
 */


/**
 * Attaches event listeners for alias mapping interactions.
 */
// This function is now imported from modals.js, so it's removed from here.

/**
 * Renders an empty grid state when no tasks are found.
 */
function renderEmptyGrid(grid, isWitty = false) {
    if (grid) {
        const lang = state.currentLang || 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        let displayMsg = I18N_DATA[lang].noTasks || 'No tasks found';

        if (isWitty && messages && messages.length > 0) {
            const randomIndex = Math.floor(Math.random() * messages.length);
            displayMsg = messages[randomIndex];
            grid.innerHTML = `
                <div class="empty-state-witty">
                    <div class="empty-icon" style="font-size: 3rem; margin-bottom: 1rem;">✨</div>
                    <div class="witty-message">${displayMsg}</div>
                </div>
            `;
        } else {
            grid.innerHTML = `
                <div class="empty-state">
                    <div class="empty-icon" style="font-size: 2rem; margin-bottom: 1rem; opacity: 0.5;">📭</div>
                    <div class="witty-message" style="opacity: 0.7;">${displayMsg}</div>
                </div>
            `;
        }
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

    if (service === 'wa') {
        const qrSection = document.getElementById('waQRSection');
        const connectedSection = document.getElementById('waConnectedSection');
        if (qrSection) qrSection.classList.toggle('hidden', isConnected);
        if (connectedSection) connectedSection.classList.toggle('hidden', !isConnected);
    }

    if (service === 'gmail') {
        const connectedInfo = document.getElementById('gmailConnectedInfo');
        const disconnectedInfo = document.getElementById('gmailDisconnectedInfo');
        if (connectedInfo) connectedInfo.classList.toggle('hidden', !isConnected);
        if (disconnectedInfo) disconnectedInfo.classList.toggle('hidden', isConnected);
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

        // "All Clear" witty message condition: Only for My Tasks tab when active count is 0 and not searching
        const isMyTasksDone = (currentTab === 'myTasksTab' && counts.my === 0 && !searchQuery);

        if (isMyTasksDone) {
            // "My Tasks" are all done. Show witty message.
            renderEmptyGrid(grid, true);
            // If there are greyed-out completed tasks, show them BELOW the message
            if (filtered.length > 0) {
                const listHtml = filtered.map(m => this.createCardElement(m)).join('');
                grid.insertAdjacentHTML('beforeend', `<div class="completed-list-divider"></div>` + listHtml);
            }
        } else if (filtered.length === 0) {
            // General empty state for other tabs or when search yields nothing
            renderEmptyGrid(grid, false);
            return;
        } else {
            grid.innerHTML = filtered.map(m => this.createCardElement(m)).join('');
        }

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
        const displayTime = TimeService.formatDisplayTime(ts, lang);
        const deadlineBadge = getDeadlineBadge(ts, m.done, lang || 'ko');

        const sourceIcon = m.source === 'slack' ? ICONS.slack : m.source === 'whatsapp' ? ICONS.whatsapp : ICONS.gmail;
        
        // Defensive: default to empty string if keys are missing
        const meText = i18n?.assigneeMe || 'Me';
        const isMe = m.assignee === 'me';
        const isInvalid = !m.assignee || m.assignee === 'undefined' || m.assignee === 'unknown';
        const assigneeText = isMe
            ? `<span class="assignee-me">${meText}</span>`
            : `<span class="assignee-other">${isInvalid ? '' : escapeHTML(m.assignee)}</span>`;

        return `
            <div class="card ${m.source} ${m.done ? 'done' : ''}" id="task-${m.id}" data-id="${m.id}">
                <div class="col-source" title="${m.source.toUpperCase()}">
                    ${sourceIcon}
                </div>
                <div class="col-room">${m.room ? `<span class="badge-room">${escapeHTML(m.room)}</span>` : '-'}</div>
                <div class="col-task">
                    <span class="task-title">${escapeHTML(m.task)}</span>
                    <div class="tag-container">
                        ${m.category === 'waiting' && !m.done ? `<span class="tag-badge waiting-tag">⏳ ${i18n.waitingTag || 'Waiting...'}</span>` : ''}
                        ${m.category === 'promise' && !m.done ? `<span class="tag-badge promise-tag">🤝 ${i18n.promiseTag || 'Commitment'}</span>` : ''}
                    </div>
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
                    <button class="action-btn delete-btn delete-task" title="${i18n?.delete || i18n?.deleteBtnText || 'Delete'}">${ICONS.delete}</button>
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

            const btnDelete = card.querySelector('.delete-task');
            if (btnDelete) {
                btnDelete.onclick = (e) => {
                    e.stopPropagation();
                    // 'id' is already defined from card.getAttribute('data-id')
                    if (id && handlers.onDeleteTask) {
                        handlers.onDeleteTask(id);
                    }
                };
            }

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
     * @param {string} type - Animation type ('classic', 'star', 'snow')
     */
    triggerConfetti(type = 'classic') {
        if (typeof confetti !== 'function') return;

        if (type === 'star') {
            confetti({
                particleCount: 100,
                spread: 70,
                origin: { y: 0.6 },
                colors: ['#FFD700', '#FDB813', '#FFFFFF'],
                shapes: ['star', 'circle']
            });
        } else if (type === 'snow') {
            confetti({
                particleCount: 150,
                spread: 100,
                origin: { y: 0.4 },
                colors: ['#ffffff', '#e0f7fa', '#b2ebf2'],
                shapes: ['circle'],
                gravity: 0.3,
                scalar: 0.7
            });
        } else {
            confetti({
                particleCount: 100,
                spread: 70,
                origin: { y: 0.6 },
                colors: ['#00d4ff', '#0052ff', '#ffffff', '#ff007f', '#32ff7e']
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
        if (xpText) xpText.textContent = `${(profile.xp || 0) % 100} / 100 XP`;
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
        if (!badge) return;

        // usage 데이터가 아예 넘어오지 않는 경우(0일 때)를 대비한 기본값 처리
        const data = usage || {};
        const todayTotal = data.todayTotal || 0;
        const monthTotal = data.monthTotal || data.monthlyTotal || 0;
        const monthCost = data.monthCost || 0;

        badge.classList.remove('hidden'); // 0일 때 숨김 처리되는 CSS 클래스 방지
        
        // 일 / 월 / 예상 비용 3가지 항목 표시
        badge.textContent = `Day: ${todayTotal.toLocaleString()} / Month: ${monthTotal.toLocaleString()} / Est: $${monthCost.toFixed(2)}`;

        // 마우스 호버 시 상세 내역 표시
        const todayPrompt = data.todayPrompt || data.dailyPrompt || 0;
        const todayComp = data.todayCompletion || data.dailyCompletion || 0;
        const tooltipText = `[오늘] 입력: ${todayPrompt.toLocaleString()} / 출력: ${todayComp.toLocaleString()}\n[이번 달] 총합: ${monthTotal.toLocaleString()}\n[예상 비용] $${monthCost.toFixed(4)}`;
        badge.setAttribute('title', tooltipText);

        // Optional: trigger subtle pop animation
        badge.style.transform = 'scale(1.02)';
        setTimeout(() => badge.style.transform = 'scale(1)', 200);
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
                    <td>${TimeService.formatDisplayTime(ts, state.currentLang)}</td>
                    <td>${compTs !== '-' ? TimeService.formatDisplayTime(compTs, state.currentLang) : '-'}</td>
                </tr>
            `;
        }).join('');
    },

    /**
     * Renders the list of user aliases in settings.
     * @param {string[]|Object} aliases - List of aliases.
     * @param {Function} onRemove - Callback function when an alias is removed.
     */
    renderAliasList(aliases, onRemove) {
        const container = document.getElementById('aliasList');
        if (!container) return;

        // 백엔드 응답이 순수 배열이 아닌 객체 { aliases: [...] } 형태일 경우 방어
        const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

        if (!list || list.length === 0) {
            container.innerHTML = '<p class="empty-list">No aliases configured</p>';
            return;
        }

        container.innerHTML = list.map(alias => `
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
     * @param {Object[]|Object} aliases - List of tenant aliases.
     * @param {Function} onRemove - Callback function when an alias is removed.
     */
    renderTenantAliasList(aliases, onRemove) {
        const container = document.getElementById('normList');
        if (!container) return;

        // 백엔드 응답이 순수 배열이 아닌 { aliases: [...] } 형태일 경우를 대비한 방어 로직
        const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

        if (!list || list.length === 0) {
            container.innerHTML = '<p class="empty-list">No tenant aliases configured</p>';
            return;
        }

        container.innerHTML = list.map(alias => {
            // 구조체 필드 이름 보정 (안전하게 값을 추출)
            const orig = alias.original_name || alias.original || alias;
            const prim = alias.primary_name || alias.primary || '';
            const displayStr = prim ? `${escapeHTML(orig)} → ${escapeHTML(prim)}` : escapeHTML(orig);

            return `
            <div class="alias-item">
                <span>${displayStr}</span>
                <button class="remove-tenant-alias-btn" data-alias="${escapeHTML(orig)}">&times;</button>
            </div>
            `;
        }).join('');

        container.querySelectorAll('.remove-tenant-alias-btn').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.dataset.alias));
        });
    },

    /**
     * Renders contact mappings in settings.
     * @param {Object[]|Object} mappings - List of mappings.
     * @param {Function} onRemove - Callback function when a mapping is removed.
     */
    renderContactMappings(mappings, onRemove) {
        const container = document.getElementById('contactList');
        if (!container) return;

        // 백엔드 응답이 { mappings: [...] } 형태일 경우를 대비
        const list = Array.isArray(mappings) ? mappings : (mappings?.mappings || mappings?.data || []);

        if (!list || list.length === 0) {
            container.innerHTML = '<p class="empty-list">No contact mappings</p>';
            return;
        }

        container.innerHTML = list.map(m => {
            // 현재 백엔드 API 규격에 맞게 필드 이름 보정
            const rep = m.rep_name || m.repName || m.name || '';
            const aliases = m.aliases || m.aliasNames || m.alias || m.source || '';

            return `
            <div class="mapping-item">
                <span class="mapping-source">${escapeHTML(aliases)}</span>
                <span class="mapping-arrow">→</span>
                <span class="mapping-target">${escapeHTML(rep)}</span>
                <button class="remove-mapping-btn" data-id="${escapeHTML(rep)}">&times;</button>
            </div>
            `;
        }).join('');

        container.querySelectorAll('.remove-mapping-btn').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.dataset.id));
        });
    },

    /**
     * Shows a non-blocking toast notification.
     * @param {string} message - Message to display.
     * @param {string} type - 'error' or 'success'.
     */
    showToast(message, type = 'error') {
        const toast = document.createElement('div');
        toast.className = `toast-popup toast-${type}`;

        const existingToasts = document.querySelectorAll('.toast-popup');
        const bottomOffset = 30 + (existingToasts.length * 70);
        toast.style.bottom = `${bottomOffset}px`;

        const icon = document.createElement('span');
        icon.textContent = type === 'error' ? '⚠️' : '✅';
        toast.appendChild(icon);

        const textNode = document.createElement('span');
        textNode.textContent = message;
        toast.appendChild(textNode);

        document.body.appendChild(toast);

        // Transition in
        requestAnimationFrame(() => {
            toast.classList.add('show');
        });

        // Transition out
        setTimeout(() => {
            toast.classList.remove('show');
            setTimeout(() => toast.remove(), 400);
        }, 3000);
    },

    /**
     * Renders release notes in the modal.
     * @param {string} content - Markdown content of release notes.
     */
    renderReleaseNotes(content) {
        const container = document.getElementById('releaseNotesContent');
        if (!container) return;

        container.innerHTML = `<div class="release-notes-markdown">${parseMarkdown(content)}</div>`;
    },

    /**
     * 스캔 버튼 및 화면의 로딩 상태를 제어합니다.
     */
    setScanLoading(isLoading, lang) {
        const btn = document.getElementById('scanBtn');
        const scanBtnIcon = document.getElementById('scanBtnIcon');
        const loading = document.getElementById('loading');

        if (btn) btn.disabled = isLoading;
        if (scanBtnIcon) scanBtnIcon.style.animation = isLoading ? 'spin 1s linear infinite' : '';
        if (loading) loading.classList.toggle('hidden', !isLoading);
    },

    /**
     * 초기 테마 상태를 UI(아이콘 및 Body 클래스)에 적용합니다.
     */
    setTheme(theme) {
        const isLight = theme === 'light';
        document.body.classList.toggle('light-theme', isLight);
        const themeToggleBtn = document.getElementById('themeToggleBtn');
        if (themeToggleBtn) {
            themeToggleBtn.innerHTML = isLight
                ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 20px; height: 20px;"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>`
                : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 20px; height: 20px;"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>`;
        }
    },

    /**
     * 테마 토글 버튼 클릭 이벤트를 바인딩합니다.
     */
    bindThemeToggle(onToggle) {
        const themeToggleBtn = document.getElementById('themeToggleBtn');
        if (!themeToggleBtn) return;
        themeToggleBtn.addEventListener('click', () => {
            const isLight = document.body.classList.toggle('light-theme');
            this.setTheme(isLight ? 'light' : 'dark');
            if (onToggle) onToggle(isLight);
        });
    },

    /**
     * WhatsApp 연결 및 QR 관련 이벤트 바인딩/UI 업데이트
     */
    bindGetQRBtn(onClick) {
        document.getElementById('getQRBtn')?.addEventListener('click', onClick);
    },

    updateWhatsAppQR(status, data, lang) {
        const btn = document.getElementById('getQRBtn');
        const img = document.getElementById('waQRImg');
        const placeholder = document.getElementById('qrPlaceholder');
        const i18n = I18N_DATA[lang || 'ko'];

        if (!btn || !img || !placeholder) return;

        if (status === 'generating') {
            btn.disabled = true;
            placeholder.textContent = i18n.generating || 'Generating...';
            placeholder.classList.remove('hidden');
            img.classList.add('hidden');
        } else if (status === 'show') {
            img.src = `data:image/png;base64,${data}`;
            img.classList.remove('hidden');
            placeholder.classList.add('hidden');
        } else if (status === 'success') {
            btn.disabled = false;
            img.classList.add('hidden');
            document.getElementById('qrTimerContainer')?.classList.add('hidden');
            placeholder.textContent = '✅ Connected!';
            placeholder.classList.remove('hidden');
            setTimeout(() => {
                document.getElementById('waLoginSection')?.classList.add('hidden');
            }, 2000);
            this.showToast(i18n.waConnected || 'WhatsApp connected!', 'success');
        } else if (status === 'error') {
            document.getElementById('qrTimerContainer')?.classList.add('hidden');
            placeholder.textContent = i18n.error || 'Error';
            this.showToast((i18n.qrError || 'Error: ') + data, 'error');
            btn.disabled = false;
        }
    },

    updateQRTimer(remaining, total) {
        const container = document.getElementById('qrTimerContainer');
        const bar = document.getElementById('qrProgressBar');
        const text = document.getElementById('qrTimerText');
        if (!container || !bar || !text) return;

        container.classList.remove('hidden');
        const percentage = (remaining / total) * 100;
        bar.style.width = `${percentage}%`;
        text.textContent = `Next refresh in ${remaining}s`;
    },

    /**
     * 정적 버튼 및 전역 위임 이벤트 바인딩
     */
    bindScanBtn(onClick) {
        document.getElementById('scanBtn')?.addEventListener('click', onClick);
    },

    bindGmailStatus(onClick) {
        document.getElementById('gmailStatusLarge')?.addEventListener('click', onClick);
    },
    
    bindWhatsAppStatus(onClick) {
        document.getElementById('waStatusLarge')?.addEventListener('click', onClick);
    },
    
    toggleWaLoginSection(show) {
        const section = document.getElementById('waLoginSection');
        if (section) {
            section.classList.toggle('hidden', !show);
        }
    },

    showGmailModal() {
        const modal = document.getElementById('gmailModal');
        if (modal) {
            modal.classList.remove('hidden');
            modal.style.display = 'flex';
        }
    },

    bindGlobalClicks(handlers) {
        document.body.addEventListener('click', (e) => {
            if (e.target && e.target.closest('#buyFreezeBtn')) {
                if (handlers.onBuyFreeze) handlers.onBuyFreeze();
            }
        });
    }
};
