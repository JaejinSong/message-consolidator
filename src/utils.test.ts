import { describe, it, expect, vi } from 'vitest';
import { escapeHTML, formatDisplayTime, safeAsync, setupTabs } from './utils.ts';

describe('utils.js - escapeHTML', () => {
    it('should escape HTML special characters', () => {
        const input = '<script>alert("xss")</script> & "quote"';
        const expected = '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt; &amp; &quot;quote&quot;';
        expect(escapeHTML(input)).toBe(expected);
    });

    it('should return empty string for null/undefined', () => {
        expect(escapeHTML(null)).toBe('');
        expect(escapeHTML(undefined)).toBe('');
    });
});

describe('utils.js - formatDisplayTime', () => {
    it('should return "방금 전" for very recent time (ko)', () => {
        const now = new Date().toISOString();
        expect(formatDisplayTime(now, 'ko')).toBe('방금 전');
    });

    it('should return "1 min. ago" for 1 minute ago (en)', () => {
        const oneMinAgo = new Date(Date.now() - 60000).toISOString();
        expect(formatDisplayTime(oneMinAgo, 'en')).toBe('1 min. ago');
    });
});

describe('utils.js - safeAsync (DOM Interaction)', () => {
    it('should NOT show login overlay when AuthError occurs by default', async () => {
        // Setup Happy DOM environment
        document.body.innerHTML = '<div id="loginOverlay" class="hidden"></div>';

        const mockFn = vi.fn().mockRejectedValue({ isAuthError: true });
        const safeFn = safeAsync(mockFn);

        try {
            await safeFn();
        } catch (_e) {
            // expected throw
        }

        const overlay = document.getElementById('loginOverlay') as HTMLElement;
        expect(overlay.classList.contains('hidden')).toBe(true);
    });

    it('should show login overlay when AuthError occurs and triggerAuthOverlay is true', async () => {
        // Setup Happy DOM environment
        document.body.innerHTML = '<div id="loginOverlay" class="hidden"></div>';

        const mockFn = vi.fn().mockRejectedValue({ isAuthError: true });
        const safeFn = safeAsync(mockFn, { triggerAuthOverlay: true });

        try {
            await safeFn();
        } catch (_e) {
            // expected throw
        }

        const overlay = document.getElementById('loginOverlay') as HTMLElement;
        expect(overlay.classList.contains('hidden')).toBe(false);
    });
});

describe('utils.js - setupTabs', () => {
    it('should toggle active class on tabs and contents correctly', () => {
        document.body.innerHTML = `
            <button class="tab" data-tab="tab1"></button>
            <button class="tab" data-tab="tab2"></button>
            <div id="tab1" class="content"></div>
            <div id="tab2" class="content"></div>
        `;

        setupTabs('.tab', '.content', 'data-tab');

        const tab1 = document.querySelector('[data-tab="tab1"]') as HTMLElement;
        const tab2 = document.querySelector('[data-tab="tab2"]') as HTMLElement;
        const content1 = document.getElementById('tab1') as HTMLElement;
        const content2 = document.getElementById('tab2') as HTMLElement;

        // Initial click
        tab1.click();
        expect(tab1.classList.contains('active')).toBe(true);
        expect(content1.classList.contains('active')).toBe(true);
        expect(tab2.classList.contains('active')).toBe(false);
        expect(content2.classList.contains('active')).toBe(false);

        // Switch click
        tab2.click();
        expect(tab1.classList.contains('active')).toBe(false);
        expect(content1.classList.contains('active')).toBe(false);
        expect(tab2.classList.contains('active')).toBe(true);
        expect(content2.classList.contains('active')).toBe(true);
    });

    it('should trigger onSwitch callback with target ID', () => {
        document.body.innerHTML = `
            <button class="tab" data-tab="targetTab"></button>
            <div id="targetTab" class="content"></div>
        `;

        const onSwitch = vi.fn();
        setupTabs('.tab', '.content', 'data-tab', 'active', onSwitch);

        (document.querySelector('.tab') as HTMLElement).click();
        expect(onSwitch).toHaveBeenCalledWith('targetTab');
    });

    it('should support BEM modifiers for settings panels', () => {
        document.body.innerHTML = `
            <button class="tab" data-tab="settingsPanel"></button>
            <div id="settingsPanel" class="content c-settings__panel"></div>
        `;

        setupTabs('.tab', '.content', 'data-tab');

        (document.querySelector('.tab') as HTMLElement).click();
        const panel = document.getElementById('settingsPanel') as HTMLElement;
        expect(panel.classList.contains('c-settings__panel--active')).toBe(true);
    });
});
