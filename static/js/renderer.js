import { state } from './state.js';
import { I18N_DATA } from './locales.js';

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
        if (date.getDate() === yesterdayDate.getDate() &&
            date.getMonth() === yesterdayDate.getMonth() &&
            date.getFullYear() === yesterdayDate.getFullYear()) {
            const i18n = I18N_DATA[lang] || I18N_DATA['en'];
            return i18n.yesterday || '어제';
        }

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
            const ydayLabels = { ko: '어제', en: 'Yesterday', id: 'Kemarin', th: 'เมื่อวาน' };
            const ydayLabel = ydayLabels[lang] || ydayLabels.en;
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
        const allGrid = document.getElementById('allTasksList');
        if (myGrid) myGrid.innerHTML = '';
        if (otherGrid) otherGrid.innerHTML = '';
        if (allGrid) allGrid.innerHTML = '';

        const data = I18N_DATA[state.currentLang];

        // Keywords for local filtering
        const uniqueKeywords = [];
        if (state.userProfile.email && state.userProfile.email.trim()) {
            const lowEmail = state.userProfile.email.trim().toLowerCase();
            uniqueKeywords.push(lowEmail);
            if (lowEmail.includes('@')) {
                const prefix = lowEmail.split('@')[0];
                uniqueKeywords.push(prefix);
            }
        }
        if (state.userProfile.name && state.userProfile.name.trim()) {
            const lowName = state.userProfile.name.trim().toLowerCase();
            uniqueKeywords.push(lowName);
            const noSpaceName = lowName.replace(/\s+/g, '');
            if (noSpaceName !== lowName) {
                uniqueKeywords.push(noSpaceName);
            }
        }

        if (state.userAliases && Array.isArray(state.userAliases)) {
            state.userAliases.forEach(alias => {
                if (alias && typeof alias === 'string' && alias.trim()) {
                    const lowAlias = alias.trim().toLowerCase();
                    uniqueKeywords.push(lowAlias);
                    // 띄어쓰기 제거 버전도 추가하여 normalizedAssignee와 비교 가능하게 함
                    const noSpaceAlias = lowAlias.replace(/\s+/g, '');
                    if (noSpaceAlias !== lowAlias) {
                        uniqueKeywords.push(noSpaceAlias);
                    }
                }
            });
        }

        // 일반 지칭 대명사 (완전 일치 검사용 - 오탐 방지)
        const genericKeywords = ['내업무', '사용자', '나', 'me', 'user', 'mytasks'];
        if (data && data.myTasks) {
            genericKeywords.push(data.myTasks.replace(/\s+/g, '').toLowerCase());
        }

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
            if (allGrid) allGrid.innerHTML = noMsg;
            this.updateCounts(0, 0, 0);
            return;
        }

        const activeMessages = messages.filter(m => !m.is_deleted);
        const sorted = activeMessages.sort((a, b) => {
            const aDone = !!a.done;
            const bDone = !!b.done;
            if (aDone !== bDone) return aDone ? 1 : -1;
            return new Date(b.created_at) - new Date(a.created_at);
        });

        let myCount = 0, otherCount = 0, allCount = 0;
        let myPendingCount = 0;

        sorted.forEach(m => {
            const rawAssignee = (m.assignee || "").trim();
            const normalizedAssignee = rawAssignee.replace(/\s+/g, '').toLowerCase();
            const normalizedRequester = (m.requester || "").replace(/\s+/g, '').toLowerCase();
            let isMyTask = false;

            // 1. Explicit labels & Direct Name/Alias match
            const isExplicitMine = normalizedAssignee === '내업무' || normalizedAssignee === 'mytasks' ||
                rawAssignee === (data && data.myTasks) ||
                uniqueKeywords.includes(normalizedAssignee);

            if (isExplicitMine) {
                isMyTask = true;
            } else if (normalizedAssignee === '기타업무' || normalizedAssignee === 'othertasks' || rawAssignee === (data && data.otherTasks)) {
                isMyTask = false;
            } else {
                // 2. Keyword matching
                // 2-1. 고유 식별자는 업무 본문, 담당자, 요청자에 유연하게 포함되어 있는지 검사
                isMyTask = uniqueKeywords.some(kw => {
                    if (!kw) return false;
                    const lowKw = kw.toLowerCase();
                    return (m.task && m.task.toLowerCase().includes(lowKw)) ||
                        (rawAssignee && rawAssignee.toLowerCase().includes(lowKw)) ||
                        (m.requester && m.requester.toLowerCase().includes(lowKw));
                });

                // 2-2. 일반 지칭 대명사('나', 'me' 등)는 담당자나 요청자 이름과 '정확히 일치'할 때만 내 업무로 처리
                if (!isMyTask) {
                    isMyTask = genericKeywords.includes(normalizedAssignee) || genericKeywords.includes(normalizedRequester);
                }
            }

            if (isMyTask && m.id === 7512) {
                console.log("[DEBUG] isMyTask=TRUE for 7512", { normalizedAssignee, uniqueKeywords, isExplicitMine });
            } else if (m.id === 7512) {
                console.log("[DEBUG] isMyTask=FALSE for 7512", { normalizedAssignee, uniqueKeywords, isExplicitMine });
            }

            const cardAll = this.createCardElement(m, data, handlers);
            if (allGrid) allGrid.appendChild(cardAll);
            allCount++;

            const cardFiltered = this.createCardElement(m, data, handlers);
            if (isMyTask) {
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

        if (myCount > 0 && myPendingCount === 0) {
            const randomMsg = data.emptyStateMessages[Math.floor(Math.random() * data.emptyStateMessages.length)];
            const wittyEl = document.createElement('div');
            wittyEl.className = 'empty-state-witty small';
            wittyEl.innerHTML = `<div class="witty-message">${randomMsg}</div>`;
            if (myGrid) {
                myGrid.insertBefore(wittyEl, myGrid.firstChild);
            }
        }

        this.updateCounts(myCount, otherCount, allCount);
    },

    updateCounts(my, other, all) {
        const myCountEl = document.getElementById('myCount');
        const otherCountEl = document.getElementById('otherCount');
        const allCountEl = document.getElementById('allCount');
        if (myCountEl) myCountEl.textContent = my;
        if (otherCountEl) otherCountEl.textContent = other;
        if (allCountEl) allCountEl.textContent = all;
    },

    createCardElement(m, data, handlers) {
        const card = document.createElement('div');
        card.className = `card ${m.source} ${m.done ? 'done' : ''}`;

        const viewOriginalIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                <circle cx="12" cy="12" r="3"></circle>
            </svg>`;

        const linkIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
                <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
                <polyline points="15 3 21 3 21 9"></polyline>
                <line x1="10" y1="14" x2="21" y2="3"></line>
            </svg>`;

        const doneIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" style="width: 18px; height: 18px;">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>`;

        const deleteIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
            </svg>`;

        let actionBtnHtml = '';

        // 1. View Original Modal (Eye icon)
        if (m.original_text) {
            actionBtnHtml += `<button type="button" class="action-btn original-btn" data-id="${m.id}" title="${data.viewOriginal}">${viewOriginalIcon}</button>`;
        }

        // 2. Direct Link to Source (External Icon) - Removed for Gmail as requested
        if (m.link && m.source.toLowerCase() !== 'gmail') {
            const openInText = data.openIn ? data.openIn.replace('{source}', m.source) : `Open in ${m.source}`;
            actionBtnHtml += `<a href="${m.link}" target="_blank" class="action-btn link-btn" title="${openInText}">${linkIcon}</a>`;
        }

        let sourceIcon = this.getSourceIcon(m.source);

        card.innerHTML = `
            <div class="col-source" title="${m.source}">${sourceIcon || '<span class="badge">' + m.source + '</span>'}</div>
            <div class="col-room" title="${escapeHTML(m.room)}"><span class="badge-room">${escapeHTML(m.room) || '-'}</span></div>
            <div class="col-task task-title" title="${escapeHTML(m.task)}">${escapeHTML(m.task)}</div>
            <div class="col-requester meta-val clickable-name" title="${data.clickToMapAlias || 'Click to map alias: '}${escapeHTML(m.requester)}">${escapeHTML(m.requester)}</div>
            <div class="col-assignee meta-val clickable-name" title="${data.clickToMapAlias || 'Click to map alias: '}${escapeHTML(m.assignee)}">${escapeHTML(m.assignee)}</div>
            <div class="col-time meta-val" style="font-size: 0.75rem;">${formatDisplayTime(m.assigned_at, state.currentLang)}</div>
            <div class="col-actions">
                ${actionBtnHtml}
                <button type="button" class="action-btn done-btn" data-id="${m.id}" data-done="${!m.done}" title="${m.done ? data.doneBtn : data.markDone}">
                    ${doneIcon}
                </button>
                <button type="button" class="action-btn delete-btn" data-id="${m.id}" title="${data.deleteBtnText || 'Delete'}">
                    ${deleteIcon}
                </button>
            </div>
        `;

        const originalBtn = card.querySelector('.original-btn');
        if (originalBtn && m.original_text) {
            originalBtn.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
                window.showOriginalMessage(m.original_text);
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
        if (source.toLowerCase() === 'slack') {
            return `<svg viewBox="0 0 100 100" style="width: 20px; height: 20px;">
                <path fill="#E01E5A" d="M22.9,53.8V42.3c0-3.2-2.6-5.8-5.8-5.8s-5.8,2.6-5.8,5.8v11.5c0,3.2,2.6,5.8,5.8,5.8S22.9,57,22.9,53.8z"/>
                <path fill="#E01E5A" d="M28.6,42.3c0-3.2,2.6-5.8,5.8-5.8h11.5c3.2,0,5.8,2.6,5.8,5.8s-2.6,5.8-5.8,5.8H34.4C31.2,48.1,28.6,45.5,28.6,42.3z"/>
                <path fill="#36C5F0" d="M46.2,22.9h11.5c3.2,0,5.8-2.6,5.8-5.8s-2.6-5.8-5.8-5.8H46.2c-3.2,0-5.8,2.6-5.8,5.8S43,22.9,46.2,22.9z"/>
                <path fill="#36C5F0" d="M57.7,28.6c3.2,0,5.8,2.6,5.8,5.8v11.5c0,3.2-2.6,5.8-5.8,5.8s-5.8-2.6-5.8-5.8V34.4C51.9,31.2,54.5,28.6,57.7,28.6z"/>
                <path fill="#2EB67D" d="M77.1,46.2v11.5c0,3.2,2.6,5.8,5.8,5.8s5.8-2.6,5.8-5.8V46.2c0-3.2-2.6-5.8-5.8-5.8S77.1,43,77.1,46.2z"/>
                <path fill="#2EB67D" d="M71.4,57.7c0,3.2-2.6,5.8-5.8,5.8H54.1c-3.2,0-5.8-2.6-5.8-5.8s2.6-5.8,5.8-5.8h11.5C68.8,51.9,71.4,54.5,71.4,57.7z"/>
                <path fill="#ECB22E" d="M53.8,77.1H42.3c-3.2,0-5.8,2.6-5.8,5.8s2.6,5.8,5.8,5.8h11.5c3.2,0,5.8-2.6,5.8-5.8S57,77.1,53.8,77.1z"/>
                <path fill="#ECB22E" d="M42.3,71.4c-3.2,0-5.8-2.6-5.8-5.8V54.1c0-3.2,2.6-5.8,5.8-5.8c3.2,0,5.8,2.6,5.8,5.8v11.5C48.1,68.8,45.5,71.4,42.3,71.4z"/>
            </svg>`;
        } else if (source.toLowerCase() === 'whatsapp') {
            return `<svg viewBox="0 0 448 512" style="width: 20px; height: 20px; fill: #25d366;">
                <path d="M380.9 97.1C339 55.1 283.2 32 223.9 32c-122.4 0-222 99.6-222 222 0 39.1 10.2 77.3 29.6 111L0 480l117.7-30.9c32.4 17.7 68.9 27 106.1 27h.1c122.3 0 224.1-99.6 224.1-222 0-59.3-25.2-115-67.1-157zm-157 341.6c-33.2 0-65.7-8.9-94-25.7l-6.7-4-69.8 18.3L72 359.2l-4.4-7c-18.5-29.4-28.2-63.3-28.2-98.2 0-101.7 82.8-184.5 184.6-184.5 49.3 0 95.6 19.2 130.4 54.1 34.8 34.9 56.2 81.2 56.1 130.5 0 101.8-84.9 184.6-186.6 184.6zm101.2-138.2c-5.5-2.8-32.8-16.2-37.9-18-5.1-1.9-8.8-2.8-12.5 2.8-3.7 5.6-14.3 18-17.6 21.8-3.2 3.7-6.5 4.2-12 1.4-5.5-2.8-23.2-8.5-44.2-27.1-16.4-14.6-27.4-32.7-30.6-38.2-3.2-5.6-.3-8.6 2.4-11.3 2.5-2.4 5.5-6.5 8.3-9.7 2.8-3.3 3.7-5.6 5.6-9.3 1.8-3.7.9-6.9-.5-9.7-1.4-2.8-12.5-30.1-17.1-41.2-4.5-10.8-9.1-9.3-12.5-9.5-3.2-.2-6.9-.2-10.6-.2-3.7 0-9.7 1.4-14.8 6.9-5.1 5.6-19.4 19-19.4 46.3 0 27.3 19.9 53.7 22.6 57.4 2.8 3.7 39.1 59.7 94.8 83.8 13.2 5.7 23.5 9.2 31.6 11.8 13.3 4.2 25.4 3.6 35 2.2 10.7-1.6 32.8-13.4 37.4-26.4 4.6-13 4.6-24.1 3.2-26.4-1.3-2.5-5-3.9-10.5-6.6z"/>
            </svg>`;
        } else if (source.toLowerCase() === 'gmail') {
            return `<svg viewBox="0 0 512 512" style="width: 20px; height: 20px;">
                <path fill="#EA4335" d="M48 64C21.5 64 0 85.5 0 112v288c0 26.5 21.5 48 48 48h416c26.5 0 48-21.5 48-48V112c0-26.5-21.5-48-48-48H48zM48 96h416c8.8 0 16 7.2 16 16v21.3L256 295.1 32 133.3V112c0-8.8 7.2-16 16-16zm-16 70.6l208 147.3L448 166.6V400c0 8.8-7.2 16-16 16H80c-8.8 0-16-7.2-16-16V166.6z"/>
            </svg>`;
        }
        return '';
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

            return `
                <tr>
                    <td><input type="checkbox" class="archive-check" data-id="${m.id}"></td>
                    <td>
                        <div title="${escapeHTML(m.source)}" style="display: flex; justify-content: center;">
                            ${sourceIcon || '<span class="badge">' + escapeHTML(m.source) + '</span>'}
                        </div>
                    </td>
                    <td class="archive-room">${escapeHTML(m.room)}</td>
                    <td class="archive-task">${escapeHTML(m.task)}</td>
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

    updateUserProfile(profile) {
        const imgEl = document.getElementById('userAvatar');

        if (profile.email) {
            if (imgEl) {
                imgEl.src = profile.picture || 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y';
                imgEl.title = profile.name || profile.email;
            }
        }
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

    updateSlackStatus(messages) {
        const slackIcon = document.getElementById('slackStatusLarge');
        const slackText = document.getElementById('slackStatusText');
        if (!slackIcon) return;

        const hasSlack = messages.some(m => m.source.toLowerCase() === 'slack');
        if (hasSlack) {
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

            const tooltipTitle = (i18n.tokenTooltipToday || "Today Usage")
                .replace('{prompt}', prompt.toLocaleString())
                .replace('{completion}', completion.toLocaleString());

            const tooltipMonth = (i18n.tokenTooltipMonth || "Month Total")
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
