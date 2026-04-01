import { describe, it, expect, beforeEach } from 'vitest';
import { updateUILanguage } from './i18n.js';
import { I18N_DATA } from './locales.js';

describe('i18n.js - updateUILanguage', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div data-i18n="myTasks">My Tasks</div>
            <button data-i18n-title="deleteBtnText" title="Delete"></button>
            <input data-i18n-placeholder="archiveSearchPlaceholder" placeholder="Search...">
            
            <!-- Main Nav Section -->
            <button class="c-main-nav__item" data-tab="myTasksTab">Dashboard</button>
            
            <!-- Category Tabs Section -->
            <button class="tab-btn c-tabs__btn" data-tab="myTasksTab">
                <span data-i18n="myTasks">My Tasks</span>
                <span class="badge count" id="myCount">10</span>
            </button>
            
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

    it('should maintain badges in tabs when updating language for both nav and tabs', () => {
        updateUILanguage('ko');
        
        // 1. Verify Category Tab (with badge)
        const categoryTab = document.querySelector('.tab-btn[data-tab="myTasksTab"]');
        expect(categoryTab.innerHTML).toContain(I18N_DATA['ko'].myTasks);
        expect(categoryTab.querySelector('#myCount').textContent).toBe('10');

        // 2. Verify Main Nav Link (no badge)
        const mainNav = document.querySelector('.c-main-nav__item[data-tab="myTasksTab"]');
        expect(mainNav.textContent).toBe(I18N_DATA['ko'].dashboardTitle);
    });

    it('should update status text based on active class', () => {
        updateUILanguage('ko');
        const slackText = document.getElementById('slackStatusText');
        expect(slackText.textContent).toBe(I18N_DATA['ko'].statusOn);

        document.getElementById('slackStatusLarge').classList.remove('active');
        updateUILanguage('ko');
        expect(slackText.textContent).toBe(I18N_DATA['ko'].statusOff);
    });

    it('should preserve DOM structure after multiple language toggles', () => {
        // 1. Initial toggle to KO
        updateUILanguage('ko');
        let categoryTab = document.querySelector('.tab-btn[data-tab="myTasksTab"]');
        expect(categoryTab.innerHTML).toContain(I18N_DATA['ko'].myTasks);
        expect(categoryTab.querySelector('[data-i18n="myTasks"]')).not.toBeNull();

        // 2. Toggle back to EN (assuming English labels are available)
        // Note: For test simplicity, we check if it doesn't crash and still has the span
        updateUILanguage('en');
        categoryTab = document.querySelector('.tab-btn[data-tab="myTasksTab"]');
        expect(categoryTab.querySelector('[data-i18n="myTasks"]')).not.toBeNull();

        // 3. Toggle back to KO again
        updateUILanguage('ko');
        categoryTab = document.querySelector('.tab-btn[data-tab="myTasksTab"]');
        expect(categoryTab.innerHTML).toContain(I18N_DATA['ko'].myTasks);
        expect(categoryTab.querySelector('[data-i18n="myTasks"]')).not.toBeNull();
    });
});

