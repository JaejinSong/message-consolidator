import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderer } from './renderer.js';
import { I18N_DATA } from './locales.js';
import { insightsRenderer } from './insightsRenderer.js';

describe('renderer.js - Empty State Messages', () => {
    it('should have a sufficient number of witty messages', () => {
        const lang = 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        expect(messages.length).toBeGreaterThanOrEqual(15);
        expect(messages.some(m => m.includes('커피'))).toBe(true);
    });
});

describe('renderer.js - updateTokenBadge', () => {
    beforeEach(() => {
        document.body.innerHTML = '<div id="tokenUsageBadge"></div>';
    });

    it('should show Day: 0 / Month: 0 / Est: $0.00 when usage is null', () => {
        renderer.updateTokenBadge(null);
        const badge = document.getElementById('tokenUsageBadge');
        expect(badge.classList.contains('hidden')).toBe(false);
        expect(badge.textContent).toBe('Day: 0 / Month: 0 / Est: $0.00');
    });

    it('should format numbers with commas', () => {
        renderer.updateTokenBadge({ todayTotal: 1500, todayPrompt: 1000, todayCompletion: 500, monthTotal: 50000 });
        const badge = document.getElementById('tokenUsageBadge');
        expect(badge.textContent).toBe('Day: 1,500 / Month: 50,000 / Est: $0.00');
        expect(badge.getAttribute('title')).toContain('50,000');
    });
});

describe('renderer.js - showToast', () => {
    it('should create and append a toast element', () => {
        renderer.showToast('Test Message', 'success');
        const toast = document.querySelector('.toast-popup');
        expect(toast).not.toBeNull();
        expect(toast.classList.contains('toast-success')).toBe(true);
        expect(toast.textContent).toContain('Test Message');
    });
});

describe('insightsRenderer.js', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="dailyGlance"></div>
            <div id="waitingMetrics"></div>
            <div id="achievementsList"></div>
        `;
    });

    it('should render daily glance stats', () => {
        const mockStats = { total_completed: 42, peak_time: '14:00', abandoned_tasks: 3 };
        insightsRenderer.renderDailyGlance(mockStats);
        const glance = document.getElementById('dailyGlance');
        expect(glance.innerHTML).toContain('42');
        expect(glance.innerHTML).toContain('14:00');
    });

    it('should render achievements with i18n names', () => {
        const mockAllAch = [
            { id: 1, name: "Task Master 10", description: "Completed 10 tasks.", criteria_type: "total_tasks", target_value: 10, icon: "🥉" }
        ];
        const mockUserAch = [{ achievement_id: 1 }];
        const mockStats = { total_completed: 12 };

        insightsRenderer.renderAchievements(mockAllAch, mockUserAch, mockStats);
        const list = document.getElementById('achievementsList');
        expect(list.innerHTML).toContain('태스크 마스터 (10)');
    });
});

describe('renderer.js - setScanLoading', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <button id="scanBtn"></button>
            <i id="scanBtnIcon"></i>
            <div id="loading" class="hidden"></div>
        `;
    });

    it('should toggle loading state', () => {
        renderer.setScanLoading(true, 'ko');
        expect(document.getElementById('scanBtn').disabled).toBe(true);
        expect(document.getElementById('loading').classList.contains('hidden')).toBe(false);

        renderer.setScanLoading(false, 'ko');
        expect(document.getElementById('scanBtn').disabled).toBe(false);
        expect(document.getElementById('loading').classList.contains('hidden')).toBe(true);
    });
});

describe('renderer.js - createCardElement', () => {
    it('should include promise tag when category is promise', () => {
        const msg = { id: 1, source: 'slack', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'promise', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('promise-tag');
        expect(html).toContain('🤝');
    });

    it('should include waiting tag when category is waiting', () => {
        const msg = { id: 2, source: 'whatsapp', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'waiting', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('waiting-tag');
        expect(html).toContain('⏳');
    });

    it('should use assignee-me class for current user', () => {
        const msg = { id: 3, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'me', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('assignee-me');
    });

    it('should handle literal "undefined" or "unknown" assignee by not rendering it', () => {
        const msgUndef = { id: 4, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'undefined', room: 'R' };
        const htmlUndef = renderer.createCardElement(msgUndef);
        expect(htmlUndef).not.toContain('undefined');

        const msgUnknown = { id: 5, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'unknown', room: 'R' };
        const htmlUnknown = renderer.createCardElement(msgUnknown);
        expect(htmlUnknown).not.toContain('unknown');
    });

    it('should escape HTML in task, requester, and room to prevent XSS', () => {
        const xssMsg = { 
            id: 6, 
            source: 'slack', 
            task: '<script>alert("xss")</script>', 
            requester: '<b>Attacker</b>', 
            room: '<img src=x onerror=alert(1)>',
            timestamp: new Date().toISOString(), 
            done: false 
        };
        const html = renderer.createCardElement(xssMsg);
        expect(html).toContain('&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;');
        expect(html).toContain('&lt;b&gt;Attacker&lt;/b&gt;');
        expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
        expect(html).not.toContain('<script>');
        expect(html).not.toContain('<b>');
    });
});

describe('renderer.js - updateUserProfile', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="userXP"></div>
            <div id="xpText"></div>
            <div id="xpBar"></div>
            <div id="userPoints"></div>
            <div id="userLevel"></div>
            <div id="userStreak"></div>
        `;
    });

    it('should display relative XP progress (modulo 100)', () => {
        renderer.updateUserProfile({ xp: 165, level: 2, streak: 5, points: 50 });
        expect(document.getElementById('xpText').textContent).toBe('65 / 100 XP');
        expect(document.getElementById('xpBar').style.width).toBe('65%');
    });

    it('should handle zero or null XP', () => {
        renderer.updateUserProfile({ xp: null, level: 1, streak: 0, points: 0 });
        expect(document.getElementById('xpText').textContent).toBe('0 / 100 XP');
        expect(document.getElementById('xpBar').style.width).toBe('0%');
    });
});

