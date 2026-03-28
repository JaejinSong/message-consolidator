import { state } from './state.js';
import { I18N_DATA } from './locales.js';

export const updateUILanguage = (lang) => {
    const data = I18N_DATA[lang];
    if (!data) return;

    const translateAttribute = (attribute, property) => {
        document.querySelectorAll(`[${attribute}]`).forEach(el => {
            const key = el.getAttribute(attribute);
            if (data[key]) {
                el[property] = data[key];
            }
        });
    };

    // Generic translation for elements with data-i18n attributes.
    translateAttribute('data-i18n', 'textContent');
    translateAttribute('data-i18n-title', 'title');
    translateAttribute('data-i18n-placeholder', 'placeholder');

    // Why: This function updates only the text label of a tab, intentionally leaving the
    // count badge (`<span class="count">`) untouched. The badge's content is managed
    // by a separate rendering process (renderer.js) to prevent data overwrites.
    const updateCountBadge = (tabSelector, textKey) => {
        const tab = document.querySelector(`.tab-btn[data-tab="${tabSelector}"]`);
        if (tab) {
            const labelEl = tab.querySelector(`[data-i18n="${textKey}"]`);
            if (labelEl && data[textKey]) {
                labelEl.textContent = data[textKey];
            }
        }
    };

    const updateMainNavLink = (tabSelector, textKey) => {
        const tab = document.querySelector(`.c-main-nav__item[data-tab="${tabSelector}"]`);
        if (tab) {
            tab.textContent = data[textKey] || tab.textContent;
        }
    };

    // Main navigation is simple text replacement.
    updateMainNavLink('myTasksTab', 'dashboardTitle');
    updateMainNavLink('archiveLink', 'archiveTitle');
    updateMainNavLink('insightsLink', 'insightsTitle');

    // Category tabs require special handling to preserve the count badge.
    updateCountBadge('myTasksTab', 'myTasks');
    updateCountBadge('otherTasksTab', 'otherTasks');
    updateCountBadge('waitingTasksTab', 'waitingTasks');
    updateCountBadge('allTasksTab', 'allTasks');

    // Why: Reconstruct the innerHTML to update the title text while preserving the count badge element,
    // which is managed separately by the archive renderer.
    const archiveHeaderTitle = document.querySelector('#archiveSection h2');
    if (archiveHeaderTitle) {
        const archiveCount = document.getElementById('archiveCount');
        const countText = archiveCount ? archiveCount.outerHTML : '';
        archiveHeaderTitle.innerHTML = `${data.archiveTitle} ${countText}`;
    }

    // Update dynamic status text (e.g., "ON" / "OFF") based on the 'active' class of the icon.
    const slackIcon = document.getElementById('slackStatusLarge');
    const waIcon = document.getElementById('waStatusLarge');
    if (slackIcon) {
        const slackText = document.getElementById('slackStatusText');
        if (slackText) slackText.textContent = slackIcon.classList.contains('active') ? data.statusOn : data.statusOff;
    }
    if (waIcon) {
        const waText = document.getElementById('waStatusText');
        if (waText) waText.textContent = waIcon.classList.contains('active') ? data.statusOn : data.statusOff;
    }
};
