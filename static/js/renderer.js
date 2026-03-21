import { state } from './state.js';
import { I18N_DATA } from './locales.js';
import { createTaskFilter } from './taskFilter.js';
import { ICONS } from './icons.js';

window.showOriginalMessage = function (text) {
    const modal = document.getElementById('originalMessageModal');
    const content = document.getElementById('originalTextContent');
    if (modal && content) {
        content.textContent = text;
        modal.classList.remove('hidden');
    }
};

const formatDisplayTime = (isoStr, lang) => {
    if (!isoStr) return '-';

    let dateStr = isoStr;
    // Handle legacy suffix from database
    if (typeof dateStr === 'string') {
        if (dateStr.includes(' KST')) dateStr = dateStr.replace(' KST', ' +0900');
        else if (dateStr.includes(' JKT')) dateStr = dateStr.replace(' JKT', ' +0700');
        else if (dateStr.includes(' ICT')) dateStr = dateStr.replace(' ICT', ' +0700');
        else if (dateStr.match(/^\d{2}:\d{2}$/)) {
            // If it's just HH:mm, it's likely a partial time from Gemini
            // We'll show it as is since we lack the date context in this legacy record
            return dateStr;
        }
    }

    try {
        const date = new Date(dateStr);
        if (isNaN(date.getTime())) return isoStr;

        // 상대 시간 계산 (당일 24시간 이내)
        const now = new Date();
        const diffMs = now - date;
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMins / 60);

        if (diffHours < 24 && now.getDate() === date.getDate()) {
            const i18n = I18N_DATA[lang] || I18N_DATA['en'];
            if (diffMins < 1) return i18n.justNow || '방금 전';
            if (diffMins < 60) return (i18n.minAgo || '{n}m ago').replace('{n}', diffMins);
            return (i18n.hourAgo || '{n}h ago').replace('{n}', diffHours);
        }

        const yesterdayDate = new Date(now);
        yesterdayDate.setDate(now.getDate() - 1);
        const isYesterday = (date.getDate() === yesterdayDate.getDate() &&
            date.getMonth() === yesterdayDate.getMonth() &&
            date.getFullYear() === yesterdayDate.getFullYear());

        const config = {
            ko: { offset: 9, label: 'KST' },
            id: { offset: 7, label: 'JKT' },
            th: { offset: 7, label: 'ICT' },
            en: { offset: 0, label: 'GMT' }
        };

        const { offset, label } = config[lang] || config.en;

        // Accurate timezone conversion using UTC components
        const local = new Date(date.getTime() + (3600000 * offset));

        const mm = String(local.getUTCMonth() + 1).padStart(2, '0');
        const dd = String(local.getUTCDate()).padStart(2, '0');
        const hh = String(local.getUTCHours()).padStart(2, '0');
        const min = String(local.getUTCMinutes()).padStart(2, '0');

        if (isYesterday) {
            const i18n = I18N_DATA[lang] || I18N_DATA['en'];
            const ydayLabel = i18n.yesterday || '어제';
            return `${ydayLabel} ${hh}:${min}`;
        }

        return `${mm}-${dd} ${hh}:${min} ${label}`;
    } catch (e) {
        return isoStr;
    }
};

const escapeHTML = (str) => {
    if (!str) return '';
    return String(str).replace(/[&<>'"]/g, tag => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
    }[tag]));
};

export const renderer = {
    renderMessages(messages, handlers) {
        const myGrid = document.getElementById('myTasksList');
        const otherGrid = document.getElementById('otherTasksList');
        const waitingGrid = document.getElementById('waitingTasksList');
        const allGrid = document.getElementById('allTasksList');
        if (myGrid) myGrid.innerHTML = '';
        if (otherGrid) otherGrid.innerHTML = '';
        if (waitingGrid) waitingGrid.innerHTML = '';
        if (allGrid) allGrid.innerHTML = '';

        const data = I18N_DATA[state.currentLang];
        const checkIsMyTask = createTaskFilter(state, data);

        if (!messages || messages.length === 0) {
            const noMsg = `<p style="text-align: center; color: var(--text-dim); margin-top: 2rem; width: 100%; font-size: 1.1rem;">${data.noTasks}</p>`;
            if (myGrid) {
                const randomMsg = data.emptyStateMessages[Math.floor(Math.random() * data.emptyStateMessages.length)];
                myGrid.innerHTML = `
                    <div class="empty-state-witty">
                        <div class="witty-message">${randomMsg}</div>
                        <p style="text-align: center; color: var(--text-dim); margin-top: 1rem; font-size: 0.9rem;">${data.noTasks}</p>
                    </div>`;
            }
            if (otherGrid) otherGrid.innerHTML = noMsg;
            if (waitingGrid) waitingGrid.innerHTML = noMsg;
            if (allGrid) allGrid.innerHTML = noMsg;
            this.updateCounts(0, 0, 0, 0);
            return;
        }

        const activeMessages = messages.filter(m => !m.is_deleted);
        const sorted = activeMessages.sort((a, b) => {
            const aDone = !!a.done;
            const bDone = !!b.done;
            if (aDone !== bDone) return aDone ? 1 : -1;
            return new Date(b.created_at) - new Date(a.created_at);
        });

        let myCount = 0, otherCount = 0, waitingCount = 0, allCount = 0;
        let myPendingCount = 0;

        sorted.forEach(m => {
            const isMyTask = checkIsMyTask(m);
            const cardAll = this.createCardElement(m, data, handlers);
            if (allGrid) allGrid.appendChild(cardAll);
            allCount++;

            const isWaiting = m.category === 'waiting';

            const cardFiltered = this.createCardElement(m, data, handlers);
            if (isWaiting) {
                if (waitingGrid) {
                    waitingGrid.appendChild(cardFiltered);
                    waitingCount++;
                }
            } else if (isMyTask) {
                if (myGrid) {
                    myGrid.appendChild(cardFiltered);
                    myCount++;
                    if (!m.done) myPendingCount++;
                }
            } else {
                if (otherGrid) {
                    otherGrid.appendChild(cardFiltered);
                    otherCount++;
                }
            }
        });

        // Grid-specific empty states when messages exist but current grid is empty
        if (myCount === 0) {
            const randomMsg = data.emptyStateMessages[Math.floor(Math.random() * data.emptyStateMessages.length)];
            if (myGrid) {
                myGrid.innerHTML = `
                    <div class="empty-state-witty">
                        <div class="witty-message">${randomMsg}</div>
                        <p style="text-align: center; color: var(--text-dim); margin-top: 1rem; font-size: 0.9rem;">${data.noTasks}</p>
                    </div>`;
            }
        } else if (myPendingCount === 0) {
            const randomMsg = data.emptyStateMessages[Math.floor(Math.random() * data.emptyStateMessages.length)];
            const wittyEl = document.createElement('div');
            wittyEl.className = 'empty-state-witty small';
            wittyEl.innerHTML = `<div class="witty-message">${randomMsg}</div>`;
            if (myGrid) {
                myGrid.insertBefore(wittyEl, myGrid.firstChild);
            }
        }

        if (otherCount === 0 && otherGrid) {
            otherGrid.innerHTML = `<p style="text-align: center; color: var(--text-dim); margin-top: 2rem; width: 100%; font-size: 1.1rem;">${data.noTasks}</p>`;
        }

        if (waitingCount === 0 && waitingGrid) {
            waitingGrid.innerHTML = `<p style="text-align: center; color: var(--text-dim); margin-top: 2rem; width: 100%; font-size: 1.1rem;">${data.noTasks}</p>`;
        }

        this.updateCounts(myCount, otherCount, waitingCount, allCount);
    },

    updateCounts(my, other, waiting, all) {
        const myCountEl = document.getElementById('myCount');
        const otherCountEl = document.getElementById('otherCount');
        const waitingCountEl = document.getElementById('waitingCount');
        const allCountEl = document.getElementById('allCount');
        if (myCountEl) myCountEl.textContent = my;
        if (otherCountEl) otherCountEl.textContent = other;
        if (waitingCountEl) waitingCountEl.textContent = waiting;
        if (allCountEl) allCountEl.textContent = all;
    },

    createCardElement(m, data, handlers) {
        const card = document.createElement('div');
        card.className = `card ${m.source} ${m.done ? 'done' : ''}`;

        let actionBtnHtml = '';

        // 1. View Original Modal (Eye icon)
        if (m.original_text || m.has_original) {
            actionBtnHtml += `<button type="button" class="action-btn original-btn" data-id="${m.id}" title="${data.viewOriginal}">${ICONS.viewOriginal}</button>`;
        }

        // 2. Direct Link to Source (External Icon) - Removed for Gmail as requested
        if (m.link && m.source.toLowerCase() !== 'gmail') {
            const openInText = data.openIn ? data.openIn.replace('{source}', m.source) : `Open in ${m.source}`;
            actionBtnHtml += `<a href="${m.link}" target="_blank" class="action-btn link-btn" title="${openInText}">${ICONS.link}</a>`;
        }

        let sourceIcon = this.getSourceIcon(m.source);

        const deadlineHtml = m.deadline ? `<span style="display: inline-flex; align-items: center; background: rgba(255,149,0,0.15); color: #ff9500; padding: 2px 6px; border-radius: 6px; font-size: 0.75rem; margin-left: 8px; font-weight: 600; white-space: nowrap;">⏳ ${escapeHTML(m.deadline)}</span>` : '';

        card.innerHTML = `
            <div class="col-source" title="${m.source}">${sourceIcon || '<span class="badge">' + m.source + '</span>'}</div>
            <div class="col-room" title="${escapeHTML(m.room)}"><span class="badge-room">${escapeHTML(m.room) || '-'}</span></div>
            <div class="col-task task-title" title="${escapeHTML(m.task)}">${escapeHTML(m.task)}${deadlineHtml}</div>
            <div class="col-requester meta-val clickable-name" title="${data.clickToMapAlias || 'Click to map alias: '}${escapeHTML(m.requester)}">${escapeHTML(m.requester)}</div>
            <div class="col-assignee meta-val clickable-name" title="${data.clickToMapAlias || 'Click to map alias: '}${escapeHTML(m.assignee)}">${escapeHTML(m.assignee)}</div>
            <div class="col-time meta-val" style="font-size: 0.75rem;">${formatDisplayTime(m.assigned_at, state.currentLang)}</div>
            <div class="col-actions">
                ${actionBtnHtml}
                <button type="button" class="action-btn done-btn" data-id="${m.id}" data-done="${!m.done}" title="${m.done ? data.doneBtn : data.markDone}">
                    ${ICONS.done}
                </button>
                <button type="button" class="action-btn delete-btn" data-id="${m.id}" title="${data.deleteBtnText || 'Delete'}">
                    ${ICONS.delete}
                </button>
            </div>
        `;

        const originalBtn = card.querySelector('.original-btn');
        if (originalBtn && (m.original_text || m.has_original)) {
            originalBtn.addEventListener('click', async (e) => {
                e.preventDefault();
                e.stopPropagation();

                if (m.original_text) {
                    window.showOriginalMessage(m.original_text);
                } else {
                    originalBtn.style.opacity = '0.5';
                    try {
                        const res = await fetch(`/api/messages/${m.id}/original`);
                        if (!res.ok) throw new Error(`HTTP ${res.status}`);
                        const data = await res.json();
                        m.original_text = data.original_text; // 가져온 데이터 캐싱
                        window.showOriginalMessage(m.original_text);
                    } catch (err) {
                        console.error('Failed to load original text:', err);
                        alert((I18N_DATA[state.currentLang]?.error || 'Error') + ': Failed to load original text.');
                    } finally {
                        originalBtn.style.opacity = '1';
                    }
                }
            });
        }

        card.querySelector('.done-btn').addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            handlers.onToggleDone(m.id, !m.done);
        });
        card.querySelector('.delete-btn').addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            handlers.onDeleteTask(m.id);
        });

        card.querySelectorAll('.clickable-name').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (window.openAliasMapping) window.openAliasMapping(el.textContent);
            });
        });

        return card;
    },

    getSourceIcon(source) {
        const s = source ? source.toLowerCase() : '';
        return ICONS[s] || '';
    },

    renderArchive(messages, totalCount, page, limit) {
        const body = document.getElementById('archiveBody');
        const lang = state.currentLang;
        const i18n = I18N_DATA[lang];

        // Update headers with sort indicators
        const headers = {
            'ahSource': 'source',
            'ahRoom': 'room',
            'ahTask': 'task',
            'ahRequester': 'requester',
            'ahAssignee': 'assignee',
            'ahTime': 'time',
            'ahCompletedAt': 'completed_at'
        };

        Object.keys(headers).forEach(id => {
            const el = document.getElementById(id);
            if (!el) return;

            // Clear existing indicator
            const baseText = el.textContent.replace(/[↑↓]/g, '').trim();

            if (state.archiveSort === headers[id]) {
                const arrow = state.archiveOrder === 'ASC' ? '↑' : '↓';
                el.innerHTML = `${baseText} <span style="font-size: 0.8rem; margin-left: 4px; color: var(--accent-color);">${arrow}</span>`;
                el.style.color = 'var(--accent-color)';
            } else {
                el.innerHTML = baseText;
                el.style.color = '';
            }
            el.style.cursor = 'pointer';
        });

        if (messages.length === 0) {
            body.innerHTML = `<tr><td colspan="8" style="text-align: center; padding: 2rem; color: var(--text-dim);">${i18n.noResults}</td></tr>`;
            return;
        }

        body.innerHTML = messages.map(m => {
            const time = new Date(m.created_at).toLocaleString();
            const completedAt = m.completed_at ? new Date(m.completed_at).toLocaleString() : '-';
            const sourceIcon = this.getSourceIcon(m.source);
            const deadlineHtml = m.deadline ? `<span style="display: inline-flex; align-items: center; background: rgba(255,149,0,0.15); color: #ff9500; padding: 2px 6px; border-radius: 6px; font-size: 0.75rem; margin-left: 6px; font-weight: 600;">⏳ ${escapeHTML(m.deadline)}</span>` : '';

            return `
                <tr>
                    <td><input type="checkbox" class="archive-check" data-id="${m.id}"></td>
                    <td>
                        <div title="${escapeHTML(m.source)}" style="display: flex; justify-content: center;">
                            ${sourceIcon || '<span class="badge">' + escapeHTML(m.source) + '</span>'}
                        </div>
                    </td>
                    <td class="archive-room">${escapeHTML(m.room)}</td>
                    <td class="archive-task">${escapeHTML(m.task)}${deadlineHtml}</td>
                    <td class="clickable-name">${escapeHTML(m.requester)}</td>
                    <td class="clickable-name">${escapeHTML(m.assignee)}</td>
                    <td style="font-size: 0.8rem; color: var(--text-dim);">${time}</td>
                    <td style="font-size: 0.8rem; color: var(--text-dim);">${completedAt}</td>
                </tr>
            `;
        }).join('');

        // Apply click handlers for archive names too
        body.querySelectorAll('.clickable-name').forEach(el => {
            el.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (window.openAliasMapping) window.openAliasMapping(el.textContent);
            });
        });
    },

    renderAliasList(aliases, onRemove) {
        const list = document.getElementById('aliasList');
        if (!list) return;
        list.innerHTML = '';
        aliases.forEach(alias => {
            const item = document.createElement('div');
            item.className = 'alias-item';
            item.innerHTML = `
                <span>${alias}</span>
                <button class="remove-alias" data-alias="${alias}">&times;</button>
            `;
            list.appendChild(item);
        });

        list.querySelectorAll('.remove-alias').forEach(btn => {
            btn.addEventListener('click', () => onRemove(btn.getAttribute('data-alias')));
        });
    },

    calculateXPProgress(xp) {
        const currentXP = xp || 0;
        const currentLevelXP = currentXP % 100;
        return {
            currentLevelXP,
            progress: currentLevelXP, // 1 level = 100 XP
            nextLevelXP: 100
        };
    },

    updateUserProfile(profile) {
        const imgEl = document.getElementById('userPicture');
        const gamificationStats = document.getElementById('gamificationStats');
        const userLevel = document.getElementById('userLevel');
        const userStreak = document.getElementById('userStreak');
        const userPoints = document.getElementById('userPoints');
        const xpBar = document.getElementById('xpBar');
        const xpText = document.getElementById('xpText');

        if (profile.email) {
            if (imgEl) {
                imgEl.src = profile.picture || 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y';
                imgEl.title = profile.name || profile.email;
            }

            if (gamificationStats) {
                gamificationStats.classList.remove('hidden');
                if (userLevel) userLevel.textContent = profile.level || 1;

                const streak = profile.streak || 0;
                if (userStreak) {
                    userStreak.textContent = `${streak}🔥`;
                    userStreak.classList.toggle('streak-active', streak > 0);
                }

                if (userPoints) userPoints.textContent = (profile.points || 0).toLocaleString();

                const { currentLevelXP, progress, nextLevelXP } = this.calculateXPProgress(profile.xp);

                if (xpBar) xpBar.style.width = `${progress}%`;
                if (xpText) xpText.textContent = `${currentLevelXP} / ${nextLevelXP} XP`;
            }
        }
    },

    triggerConfetti() {
        // 10% 확률로만 화려한 꽃가루 터뜨리기 (가변적 보상을 통한 중독성 극대화)
        if (typeof confetti === 'function' && Math.random() < 0.10) {
            confetti({
                particleCount: 150,
                spread: 70,
                origin: { y: 0.6 },
                colors: ['#00f2ff', '#bc00ff', '#25d366', '#ff9500']
            });
        }
    },

    triggerXPAnimation() {
        const statsEl = document.getElementById('gamificationStats');

        // 연속 클릭 시 완벽히 겹치는 것을 방지하기 위한 미세한 난수 오프셋
        const offsetX = Math.random() * 30 - 15;
        const offsetY = Math.random() * 15;

        let topPos = 80 + offsetY;
        let rightPos = (window.innerWidth / 2) - 50 + offsetX; // 기본값 (중앙 상단)

        if (statsEl && !statsEl.classList.contains('hidden')) {
            const rect = statsEl.getBoundingClientRect();
            topPos = rect.bottom + 15 + offsetY;
            // 모바일에서는 중앙 근처, 데스크탑에서는 요소 근처
            if (window.innerWidth < 480) {
                rightPos = (window.innerWidth - 200) / 2 + offsetX; // 200 is approx width of floatEl
            } else {
                rightPos = window.innerWidth - rect.right + 20 + offsetX;
            }
        }

        // Ensure rightPos is not negative and stays within viewport
        rightPos = Math.max(10, Math.min(rightPos, window.innerWidth - 210));

        const floatEl = document.createElement('div');
        floatEl.innerHTML = `<span style="color: #ff9500; font-weight: 800; margin-right: 12px; text-shadow: 0 2px 4px rgba(0,0,0,0.5);">+10 XP</span>
                             <span style="color: #00f2ff; font-weight: 800; text-shadow: 0 2px 4px rgba(0,0,0,0.5);">+5 PTS</span>`;
        floatEl.style.position = 'fixed';
        floatEl.style.top = `${topPos}px`;
        floatEl.style.right = `${rightPos}px`;
        floatEl.style.padding = '8px 16px';
        floatEl.style.background = 'rgba(20, 20, 25, 0.85)';
        floatEl.style.border = '1px solid rgba(255,255,255,0.15)';
        floatEl.style.borderRadius = '20px';
        floatEl.style.boxShadow = '0 8px 16px rgba(0,0,0,0.3)';
        floatEl.style.zIndex = '9999';
        floatEl.style.opacity = '0';
        floatEl.style.transform = 'translateY(20px) scale(0.8)';
        floatEl.style.transition = 'all 0.5s cubic-bezier(0.175, 0.885, 0.32, 1.275)';
        floatEl.style.pointerEvents = 'none'; // 마우스 클릭 방해 방지

        document.body.appendChild(floatEl);
        void floatEl.offsetWidth; // Trigger reflow

        floatEl.style.opacity = '1';
        floatEl.style.transform = 'translateY(0) scale(1)';

        setTimeout(() => {
            floatEl.style.opacity = '0';
            floatEl.style.transform = 'translateY(-30px) scale(0.9)';
            setTimeout(() => floatEl.remove(), 500); // DOM에서 완전 제거
        }, 1200);
    },

    updateWhatsAppStatus(status) {
        const waIcon = document.getElementById('waStatusLarge');
        const waText = document.getElementById('waStatusText');
        const waLoginSection = document.getElementById('waLoginSection');

        if (waIcon) {
            if (status === 'CONNECTED') {
                state.waConnected = true;
                waIcon.classList.remove('inactive');
                waIcon.classList.add('active');
                if (waText) waText.textContent = I18N_DATA[state.currentLang].statusOn;
                if (waLoginSection) waLoginSection.classList.add('hidden');
            } else {
                state.waConnected = false;
                waIcon.classList.remove('active');
                waIcon.classList.add('inactive');
                if (waText) waText.textContent = I18N_DATA[state.currentLang].statusOff;
                if (waLoginSection) waLoginSection.classList.remove('hidden');
            }
        }
    },

    updateSlackStatus(connected) {
        const slackIcon = document.getElementById('slackStatusLarge');
        const slackText = document.getElementById('slackStatusText');
        if (!slackIcon) return;

        if (connected) {
            slackIcon.classList.remove('inactive');
            slackIcon.classList.add('active');
            if (slackText) slackText.textContent = I18N_DATA[state.currentLang].statusOn;
        } else {
            slackIcon.classList.remove('active');
            slackIcon.classList.add('inactive');
            if (slackText) slackText.textContent = I18N_DATA[state.currentLang].statusOff;
        }
    },

    updateGmailStatus(connected) {
        state.gmailConnected = connected;
        const gmailIcon = document.getElementById('gmailStatusLarge');
        const gmailText = document.getElementById('gmailStatusText');
        const connectedStatus = document.getElementById('gmailConnectedStatus');
        const i18n = I18N_DATA[state.currentLang] || {};

        if (gmailIcon) {
            if (connected) {
                gmailIcon.classList.remove('inactive');
                gmailIcon.classList.add('active');
                gmailIcon.style.cursor = 'default';
                gmailIcon.title = i18n.gmailStatusConnectedTitle || 'Gmail: Connected';
                if (gmailText) gmailText.textContent = I18N_DATA[state.currentLang].statusOn;
                if (connectedStatus) connectedStatus.classList.remove('hidden');
            } else {
                gmailIcon.classList.remove('active');
                gmailIcon.classList.add('inactive');
                gmailIcon.style.cursor = 'pointer';
                gmailIcon.title = i18n.gmailStatusClickToConnect || 'Gmail: Click to connect';
                if (gmailText) gmailText.textContent = I18N_DATA[state.currentLang].statusOff;
                if (connectedStatus) connectedStatus.classList.add('hidden');
            }
        }
    },

    renderTenantAliasList(aliases, onRemove) {
        const list = document.getElementById('normList');
        if (!list) return;
        list.innerHTML = '';
        Object.entries(aliases).forEach(([original, primary]) => {
            const item = document.createElement('div');
            item.className = 'alias-item';
            item.innerHTML = `
                <div class="alias-info">
                    <span class="alias-original">${original}</span>
                    <span class="alias-primary">→ ${primary}</span>
                </div>
                <button type="button" class="remove-btn">&times;</button>
            `;
            item.querySelector('.remove-btn').onclick = () => onRemove(original);
            list.appendChild(item);
        });
    },

    renderContactMappings(mappings, onRemove) {
        const list = document.getElementById('contactList');
        if (!list) return;
        list.innerHTML = '';
        mappings.forEach(({ rep_name, aliases }) => {
            const item = document.createElement('div');
            item.className = 'alias-item';
            item.innerHTML = `
                <div class="alias-info">
                    <span class="alias-original">${rep_name}</span>
                    <span class="alias-primary">${aliases}</span>
                </div>
                <button type="button" class="remove-btn">&times;</button>
            `;
            item.querySelector('.remove-btn').onclick = () => onRemove(rep_name);
            list.appendChild(item);
        });
    },

    updateTokenBadge(usage) {
        const badge = document.getElementById('tokenUsageBadge');
        if (badge) {
            const i18n = I18N_DATA[state.currentLang] || {};
            const total = usage.todayTotal || 0;
            const prompt = usage.todayPrompt || 0;
            const completion = usage.todayCompletion || 0;

            const monthTotal = usage.monthTotal || 0;
            const monthCostUSD = ((usage.monthPrompt || 0) / 1000000) * 0.075 + ((usage.monthCompletion || 0) / 1000000) * 0.3;

            // Gemini 1.5 Flash / Gemini 3 Flash Preview pricing
            // Input: $0.075/1M, Output: $0.3/1M
            const inputCostUSD = (prompt / 1000000) * 0.075;
            const outputCostUSD = (completion / 1000000) * 0.3;
            const totalCostUSD = inputCostUSD + outputCostUSD;

            // Language-based currency formatting
            let costString = '';
            if (state.currentLang === 'ko') {
                costString = `≈${(totalCostUSD * 1400).toFixed(1)}원`;
            } else {
                costString = `≈$${totalCostUSD.toFixed(3)}`;
            }

            // Compact format (e.g., 1.2M, 500K) to save UI space
            const formatTokens = (num) => {
                if (num >= 1000000) return (num / 1000000).toFixed(2) + 'M';
                if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
                return num.toString();
            };

            const monthCostString = state.currentLang === 'ko'
                ? `≈${(monthCostUSD * 1400).toFixed(1)}원`
                : `$${monthCostUSD.toFixed(3)}`;

            const tooltipTitle = (i18n.tokenTooltipToday || "[Today] Prompt: {prompt} / Completion: {completion}")
                .replace('{prompt}', prompt.toLocaleString())
                .replace('{completion}', completion.toLocaleString());

            const tooltipMonth = (i18n.tokenTooltipMonth || "[This Month] Total: {monthTotal} ({costString})")
                .replace('{monthTotal}', formatTokens(monthTotal))
                .replace('{costString}', monthCostString);

            badge.title = `${tooltipTitle}\n${tooltipMonth}`;
            badge.textContent = `Token: ${formatTokens(total)} (${costString})`;
            badge.classList.remove('hidden');


            if (total > 500000) badge.style.color = '#ff3b30';
            else if (total > 200000) badge.style.color = '#f39c12';
            else badge.style.color = 'var(--accent-light)';
        }
    },

    renderReleaseNotes(markdown) {
        const contentEl = document.getElementById('releaseNotesContent');
        if (!contentEl) return;

        // More robust markdown-like formatter for the specific RELEASE_NOTES_USER.md structure
        let html = markdown
            .replace(/^# (.*$)/gim, '<h1>$1</h1>')
            .replace(/^## (.*$)/gim, '<h2>$1</h2>')
            .replace(/^### (.*$)/gim, '<h3>$1</h3>')
            .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');

        // Handle lists logic
        const lines = html.split('\n');
        let inList = false;
        let formattedLines = [];

        lines.forEach(line => {
            const listMatch = line.match(/^[\-\*]\s+(.*)$/);
            if (listMatch) {
                if (!inList) {
                    formattedLines.push('<ul>');
                    inList = true;
                }
                formattedLines.push(`<li>${listMatch[1]}</li>`);
            } else {
                if (inList) {
                    formattedLines.push('</ul>');
                    inList = false;
                }
                if (line.trim()) {
                    // Check if it's already a tag
                    if (!line.trim().startsWith('<')) {
                        formattedLines.push(`<p>${line}</p>`);
                    } else {
                        formattedLines.push(line);
                    }
                }
            }
        });

        if (inList) formattedLines.push('</ul>');

        contentEl.innerHTML = formattedLines.join('\n');
        document.getElementById('releaseNotesModal').classList.remove('hidden');
    }
};
