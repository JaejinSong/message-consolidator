import { state } from './state.js';
import { I18N_DATA } from './i18n.js';

window.showOriginalMessage = function(text) {
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

        const config = {
            ko: { offset: 9, label: 'KST' },
            id: { offset: 7, label: 'JKT' },
            th: { offset: 7, label: 'ICT' },
            en: { offset: 0, label: 'GMT' }
        };

        const { offset, label } = config[lang] || config.en;
        
        // Accurate timezone conversion using UTC components
        const local = new Date(date.getTime() + (3600000 * offset));

        const yyyy = local.getUTCFullYear();
        const mm = String(local.getUTCMonth() + 1).padStart(2, '0');
        const dd = String(local.getUTCDate()).padStart(2, '0');
        const hh = String(local.getUTCHours()).padStart(2, '0');
        const min = String(local.getUTCMinutes()).padStart(2, '0');
        const ss = String(local.getUTCSeconds()).padStart(2, '0');

        return `${yyyy}-${mm}-${dd} ${hh}:${min}:${ss} ${label}`;
    } catch (e) {
        return isoStr;
    }
};

document.addEventListener('DOMContentLoaded', () => {
    const closeBtn = document.getElementById('closeOriginalBtn');
    const modal = document.getElementById('originalMessageModal');
    if (closeBtn && modal) {
        closeBtn.onclick = () => modal.classList.add('hidden');
        modal.onclick = (e) => {
            if (e.target === modal) modal.classList.add('hidden');
        };
    }
});

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
        const keywords = [state.userProfile.email];
        if (state.userProfile.name) keywords.push(state.userProfile.name);
        if (state.userProfile.email && state.userProfile.email.includes('@')) {
            keywords.push(state.userProfile.email.split('@')[0]);
        }
        if (state.userAliases && Array.isArray(state.userAliases)) {
            state.userAliases.forEach(alias => {
                if (alias && alias.trim()) keywords.push(alias.trim());
            });
        }

        if (!messages || messages.length === 0) {
            const noMsg = `<p style="text-align: center; color: var(--text-dim); margin-top: 1rem; width: 100%;">${data.noTasks}</p>`;
            if (myGrid) myGrid.innerHTML = noMsg;
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

        sorted.forEach(m => {
            const isMyTask = keywords.some(kw => 
                (m.task && m.task.toLowerCase().includes(kw.toLowerCase())) || 
                (m.assignee && m.assignee.toLowerCase().includes(kw.toLowerCase())) ||
                (m.requester && m.requester.toLowerCase().includes(kw.toLowerCase()))
            );

            const cardAll = this.createCardElement(m, data, handlers);
            if (allGrid) allGrid.appendChild(cardAll);
            allCount++;

            const cardFiltered = this.createCardElement(m, data, handlers);
            if (isMyTask) {
                if (myGrid) {
                    myGrid.appendChild(cardFiltered);
                    myCount++;
                }
            } else {
                if (otherGrid) {
                    otherGrid.appendChild(cardFiltered);
                    otherCount++;
                }
            }
        });
        
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
        
        const doneIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>`;
        
        const deleteIcon = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
            </svg>`;

        let actionBtnHtml = '';
        if (m.link) {
            actionBtnHtml = `<a href="${m.link}" target="_blank" class="action-btn link-btn" title="${data.viewOriginal}">${viewOriginalIcon}</a>`;
        } else if (m.original_text) {
            // Escape single quotes for the onclick handler
            const escapedText = m.original_text.replace(/'/g, "\\'");
            actionBtnHtml = `<button class="action-btn original-btn" onclick="showOriginalMessage('${escapedText}')" title="${data.viewOriginal}">${viewOriginalIcon}</button>`;
        }

        let sourceIcon = this.getSourceIcon(m.source);

        card.innerHTML = `
            <div class="col-source" title="${m.source}">${sourceIcon || '<span class="badge">' + m.source + '</span>'}</div>
            <div class="col-room meta-val" title="${m.room || ''}">${m.room || '-'}</div>
            <div class="col-task task-title" title="${m.task}">${m.task}</div>
            <div class="col-requester meta-val" title="${m.requester}">${m.requester}</div>
            <div class="col-assignee meta-val" title="${m.assignee}">${m.assignee}</div>
            <div class="col-time meta-val" style="font-size: 0.75rem;">${formatDisplayTime(m.assigned_at, state.currentLang)}</div>
            <div class="col-actions">
                ${actionBtnHtml}
                <button class="action-btn done-btn" data-id="${m.id}" data-done="${!m.done}" title="${m.done ? data.doneBtn : data.markDone}">
                    ${doneIcon}
                </button>
                <button class="action-btn delete-btn" data-id="${m.id}" title="Delete">
                    ${deleteIcon}
                </button>
            </div>
        `;

        card.querySelector('.done-btn').addEventListener('click', () => handlers.onToggleDone(m.id, !m.done));
        card.querySelector('.delete-btn').addEventListener('click', () => handlers.onDeleteTask(m.id));

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

    renderArchive(messages) {
        const body = document.getElementById('archiveBody');
        const countEl = document.getElementById('archiveCount');
        if (body) body.innerHTML = '';
        if (countEl) countEl.textContent = messages.length;

        if (!messages || messages.length === 0) {
            body.innerHTML = `<tr><td colspan="7" style="text-align: center; color: var(--text-dim);">No archived tasks.</td></tr>`;
            return;
        }

        messages.forEach(m => {
            const tr = document.createElement('tr');
            const compAt = m.completed_at ? new Date(m.completed_at).toLocaleString() : '-';
            tr.innerHTML = `
                <td><span class="badge">${m.source}</span></td>
                <td>${m.room || '-'}</td>
                <td style="font-weight: 600;">${m.task}</td>
                <td>${m.requester}</td>
                <td>${m.assignee}</td>
                <td style="font-size: 0.8rem; color: var(--text-dim);">${formatDisplayTime(m.assigned_at, state.currentLang)}</td>
                <td style="font-size: 0.8rem; color: var(--accent-color);">${m.completed_at ? formatDisplayTime(m.completed_at, state.currentLang) : '-'}</td>
            `;
            body.appendChild(tr);
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
        const profileDiv = document.getElementById('userProfile');
        const imgEl = document.getElementById('userPicture');
        const emailEl = document.getElementById('userEmail');

        if (profile.email) {
            emailEl.textContent = profile.email;
            imgEl.src = profile.picture || 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y';
            profileDiv.classList.remove('hidden');
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

        if (gmailIcon) {
            if (connected) {
                gmailIcon.classList.remove('inactive');
                gmailIcon.classList.add('active');
                gmailIcon.style.cursor = 'default';
                gmailIcon.title = 'Gmail: Connected';
                if (gmailText) gmailText.textContent = I18N_DATA[state.currentLang].statusOn;
                if (connectedStatus) connectedStatus.classList.remove('hidden');
            } else {
                gmailIcon.classList.remove('active');
                gmailIcon.classList.add('inactive');
                gmailIcon.style.cursor = 'pointer';
                gmailIcon.title = 'Gmail: Click to connect';
                if (gmailText) gmailText.textContent = I18N_DATA[state.currentLang].statusOff;
                if (connectedStatus) connectedStatus.classList.add('hidden');
            }
        }
    }
};
