import { describe, it, expect, beforeEach } from 'vitest';
import * as renderer from './renderer.ts';
import { I18N_DATA } from './locales';


describe('renderer.js - Empty State Messages', () => {
    it('should have a sufficient number of witty messages', () => {
        const lang = 'ko';
        const messages = I18N_DATA[lang].emptyStateMessages;
        expect(messages.length).toBeGreaterThanOrEqual(15);
        expect(messages.some((m: string) => m.includes('커피'))).toBe(true);
    });
});



describe('renderer.js - showToast', () => {
    it('should create and append a toast element', () => {
        renderer.showToast('Test Message', 'success');
        const toast = document.querySelector('.toast-popup') as HTMLElement;
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
        renderer.setScanLoading(true);
        expect((document.getElementById('scanBtn') as HTMLButtonElement).disabled).toBe(true);
        expect((document.getElementById('loading') as HTMLElement).classList.contains('active')).toBe(true);

        renderer.setScanLoading(false);
        expect((document.getElementById('scanBtn') as HTMLButtonElement).disabled).toBe(false);
        expect((document.getElementById('loading') as HTMLElement).classList.contains('active')).toBe(false);
    });
});

describe('renderer.js - createCardElement', () => {
    it('should include promise tag when category is promise', () => {
        const msg = { id: 1, source: 'slack', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'promise', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--promise');
        expect(html).toContain('🤝');
    });

    it('should include shared tag when category is shared', () => {
        const msg = { id: 2, source: 'whatsapp', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, category: 'shared', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__badge--shared');
        expect(html).toContain('👥');
    });

    it('should render legacy "me" assignee as a regular name', () => {
        const msg = { id: 3, source: 'gmail', task: 'Task', requester: 'Req', timestamp: new Date().toISOString(), done: false, assignee: 'me', room: 'R' };
        const html = renderer.createCardElement(msg);
        expect(html).toContain('c-message-card__assignee--other');
        expect(html).not.toContain('c-message-card__assignee--me');
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
            <div id="userProfile" class="hidden"></div>
            <div id="userEmail" class="hidden"></div>
            <img id="userPicture" src="" class="hidden">
        `;
    });

    it('should unhide userEmail and handle profile picture visibility', () => {
        // With email and picture
        renderer.updateUserProfile({
            email: 'test@example.com',
            picture: 'http://pic.jpg',
            name: 'Test User'
        });
        const emailEl = document.getElementById('userEmail') as HTMLElement;
        expect(emailEl.classList.contains('hidden')).toBe(false);
        expect(emailEl.textContent).toBe('test@example.com');
        expect((document.getElementById('userPicture') as HTMLElement).classList.contains('hidden')).toBe(false);

        // Without picture
        renderer.updateUserProfile({
            email: 'test@example.com',
            picture: '',
            name: 'Test User'
        });
        expect((document.getElementById('userPicture') as HTMLElement).classList.contains('hidden')).toBe(true);
    });

    it('should not throw error if DOM elements are missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateUserProfile({ email: 'test@example.com', picture: 'http://pic.jpg', name: 'Test User' });
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
        expect((document.getElementById('slackStatusLarge') as HTMLElement).classList.contains('c-status-card--active')).toBe(true);

        // WhatsApp Connected
        renderer.updateWhatsAppStatus(true);
        expect((document.getElementById('waQRSection') as HTMLElement).classList.contains('hidden')).toBe(true);
        expect((document.getElementById('waConnectedSection') as HTMLElement).classList.contains('hidden')).toBe(false);

        // WhatsApp Disconnected
        renderer.updateWhatsAppStatus(false);
        expect((document.getElementById('waQRSection') as HTMLElement).classList.contains('hidden')).toBe(false);
        expect((document.getElementById('waConnectedSection') as HTMLElement).classList.contains('hidden')).toBe(true);
    });

    it('should not throw error when service status DOM is completely missing', () => {
        document.body.innerHTML = '';
        expect(() => {
            renderer.updateSlackStatus(true);
            renderer.updateWhatsAppStatus(true);
        }).not.toThrow();
    });
});
