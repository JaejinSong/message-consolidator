import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as renderer from './renderer.ts';
import { I18N_DATA } from './locales.js';


describe('renderer.js - Empty State Messages', () => {
    it('should have a sufficient number of witty messages', () => {
        const lang = 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        expect(messages.length).toBeGreaterThanOrEqual(15);
        expect(messages.some(m => m.includes('커피'))).toBe(true);
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
        expect(document.getElementById('loading').classList.contains('active')).toBe(true);

        renderer.setScanLoading(false, 'ko');
        expect(document.getElementById('scanBtn').disabled).toBe(false);
        expect(document.getElementById('loading').classList.contains('active')).toBe(false);
    });
});

describe('renderer.js - createCardElement', () => {
    it('should include promise tag when category is promise', () => {
        const msg = { id: 1, source: 'slack', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'promise', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--promise');
        expect(html).toContain('🤝');
    });

    it('should include waiting tag when category is waiting', () => {
        const msg = { id: 2, source: 'whatsapp', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'waiting', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--waiting');
        expect(html).toContain('⏳');
    });

    it('should use assignee-me class for current user', () => {
        const msg = { id: 3, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'me', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__assignee--me');
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

    it('should unhide userEmail and handle profile picture visibility', () => {
        document.body.innerHTML += `
            <div id="userEmail" class="hidden"></div>
            <img id="userPicture" src="" class="hidden">
        `;
        
        // 1. With email and picture
        renderer.updateUserProfile({ 
            xp: 10, level: 1, email: 'test@example.com', 
            picture: 'http://pic.jpg', streak: 0, points: 0
        });
        const emailEl = document.getElementById('userEmail');
        expect(emailEl.classList.contains('hidden')).toBe(false);
        expect(emailEl.textContent).toBe('test@example.com');
        expect(document.getElementById('userPicture').classList.contains('hidden')).toBe(false);

        // 2. Without picture
        renderer.updateUserProfile({ 
            xp: 10, level: 1, email: 'test@example.com', 
            picture: '', streak: 0, points: 0
        });
        expect(document.getElementById('userPicture').classList.contains('hidden')).toBe(true);
    });

    it('should not throw error if DOM elements are missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateUserProfile({ xp: 10, level: 1, email: 'test@example.com', picture: 'http://pic.jpg', streak: 0, points: 0 });
        }).not.toThrow();
    });
});

describe('renderer.js - updateServiceStatusUI', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="slackStatusLarge"></div>
            <div id="slackStatusText"></div>
            <div id="waQRSection"></div>
            <div id="waConnectedSection"></div>
            <div id="gmailConnectedInfo"></div>
            <div id="gmailDisconnectedInfo"></div>
        `;
    });

    it('should toggle active classes and sections via public methods', () => {
        // Slack Connected
        renderer.updateSlackStatus(true);
        expect(document.getElementById('slackStatusLarge').classList.contains('c-status-card--active')).toBe(true);
        
        // WhatsApp Connected
        renderer.updateWhatsAppStatus(true);
        expect(document.getElementById('waQRSection').classList.contains('hidden')).toBe(true);
        expect(document.getElementById('waConnectedSection').classList.contains('hidden')).toBe(false);

        // WhatsApp Disconnected
        renderer.updateWhatsAppStatus(false);
        expect(document.getElementById('waQRSection').classList.contains('hidden')).toBe(false);
        expect(document.getElementById('waConnectedSection').classList.contains('hidden')).toBe(true);
    });

    it('should not throw error when service status DOM is completely missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateSlackStatus(true);
            renderer.updateWhatsAppStatus(true);
        }).not.toThrow();
    });
});
