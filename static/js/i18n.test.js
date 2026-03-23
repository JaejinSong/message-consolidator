import { describe, it, expect, beforeEach } from 'vitest';
import { updateUILanguage } from './i18n.js';
import { I18N_DATA } from './locales.js';

describe('i18n.js - updateUILanguage', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div data-i18n="myTasks">My Tasks</div>
            <button data-i18n-title="deleteBtnText" title="Delete"></button>
            <input data-i18n-placeholder="archiveSearchPlaceholder" placeholder="Search...">
            <div data-tab="myTasksTab"><span id="myCount">10</span></div>
            <div id="archiveSection"><h2>Archive</h2></div>
            <div id="archiveCount">5</div>
            <div id="slackStatusLarge" class="active"><span id="slackStatusText"></span></div>
        `;
    });

    it('should update textContent for elements with data-i18n', () => {
        updateUILanguage('ko');
        const el = document.querySelector('[data-i18n="myTasks"]');
        expect(el.textContent).toBe(I18N_DATA['ko'].myTasks);
    });

    it('should update title for elements with data-i18n-title', () => {
        updateUILanguage('ko');
        const el = document.querySelector('[data-i18n-title="deleteBtnText"]');
        expect(el.title).toBe(I18N_DATA['ko'].deleteBtnText);
    });

    it('should update placeholder for elements with data-i18n-placeholder', () => {
        updateUILanguage('ko');
        const el = document.querySelector('[data-i18n-placeholder="archiveSearchPlaceholder"]');
        expect(el.placeholder).toBe(I18N_DATA['ko'].archiveSearchPlaceholder);
    });

    it('should maintain badges in tabs when updating language', () => {
        updateUILanguage('ko');
        const tab = document.querySelector('[data-tab="myTasksTab"]');
        expect(tab.innerHTML).toContain(I18N_DATA['ko'].myTasks);
        expect(tab.querySelector('#myCount').textContent).toBe('10');
    });

    it('should update status text based on active class', () => {
        updateUILanguage('ko');
        const slackText = document.getElementById('slackStatusText');
        expect(slackText.textContent).toBe(I18N_DATA['ko'].statusOn);

        document.getElementById('slackStatusLarge').classList.remove('active');
        updateUILanguage('ko');
        expect(slackText.textContent).toBe(I18N_DATA['ko'].statusOff);
    });
});
