import { state } from './state.js';
import { I18N_DATA } from './i18n.js';

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
        
        let linkHtml = m.link ? `<a href="${m.link}" target="_blank" class="link-btn" style="margin-top: 0;">${data.viewOriginal}</a>` : '';
        let sourceIcon = this.getSourceIcon(m.source);

        card.innerHTML = `
            <div class="col-source" title="${m.source}">${sourceIcon || '<span class="badge">' + m.source + '</span>'}</div>
            <div class="col-room meta-val">${m.room || '-'}</div>
            <div class="col-task task-title" title="${m.task}">${m.task}</div>
            <div class="col-requester meta-val">${m.requester}</div>
            <div class="col-assignee meta-val">${m.assignee}</div>
            <div class="col-time meta-val" style="font-size: 0.75rem;">${m.assigned_at}</div>
            <div class="col-actions">
                ${linkHtml}
                <button class="done-btn" data-id="${m.id}" data-done="${!m.done}">
                    ${m.done ? data.doneBtn : data.markDone}
                </button>
                <button class="delete-btn" data-id="${m.id}">🗑️</button>
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
                <td style="font-size: 0.8rem; color: var(--text-dim);">${m.assigned_at}</td>
                <td style="font-size: 0.8rem; color: var(--accent-color);">${compAt}</td>
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
    }
};
