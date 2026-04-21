/**
 * @file i18n-renderer.ts
 * @description UI text rendering for i18n. All DOM manipulation lives here.
 * Logic (translation lookup) is delegated to src/i18n.ts.
 */

import { t, getLocale } from '../i18n';

/**
 * Applies translated text to all elements with [data-i18n] attributes.
 * Why: Centralizes DOM diffing to a single pass, avoiding redundant queries.
 */
function applyDataAttributes(lang: string): void {
    const locale = getLocale(lang);

    const apply = (attr: string, prop: 'textContent' | 'title' | 'placeholder') => {
        document.querySelectorAll<HTMLElement>(`[${attr}]`).forEach(el => {
            const key = el.getAttribute(attr);
            if (key && locale[key] != null) {
                (el as any)[prop] = locale[key];
            }
        });
    };

    apply('data-i18n', 'textContent');
    apply('data-i18n-title', 'title');
    apply('data-i18n-placeholder', 'placeholder');
}

/**
 * Updates the text label of a category tab while preserving the count badge.
 * Why: The badge is managed by a separate renderer; overwriting innerHTML would corrupt it.
 */
function applyTabLabel(tabSelector: string, textKey: string, lang: string): void {
    const tab = document.querySelector(`.tab-btn[data-tab="${tabSelector}"]`);
    if (!tab) return;
    const labelEl = tab.querySelector<HTMLElement>(`[data-i18n="${textKey}"]`);
    const text = t(textKey, lang);
    if (labelEl && text) {
        labelEl.textContent = text;
    }
}

/**
 * Updates main navigation link text.
 */
function applyNavLink(tabSelector: string, textKey: string, lang: string): void {
    const tab = document.querySelector<HTMLElement>(`.c-main-nav__item[data-tab="${tabSelector}"]`);
    if (!tab) return;
    const text = t(textKey, lang);
    if (text) tab.textContent = text;
}

/**
 * Updates the archive section header while preserving the count badge element.
 */
function applyArchiveHeader(lang: string): void {
    const archiveHeaderTitle = document.querySelector('#archiveSection h2');
    if (!archiveHeaderTitle) return;
    const archiveCount = document.getElementById('archiveCount');
    const countHtml = archiveCount ? archiveCount.outerHTML : '';
    archiveHeaderTitle.innerHTML = `${t('archiveTitle', lang)} ${countHtml}`;
}

/**
 * Updates connection status text (ON/OFF) based on active class of the icon element.
 */
function applyStatusText(iconId: string, textId: string, lang: string): void {
    const icon = document.getElementById(iconId);
    const textEl = document.getElementById(textId);
    if (!icon || !textEl) return;
    textEl.textContent = icon.classList.contains('active') ? t('statusOn', lang) : t('statusOff', lang);
}

/**
 * Main entry point. Renders all UI text for the given language.
 * Called by app.ts on init and on language change.
 */
export function renderUILanguage(lang: string): void {
    const safeLang = lang || 'en';

    applyDataAttributes(safeLang);

    applyNavLink('receivedTasksTab', 'dashboardTitle', safeLang);
    applyNavLink('archiveLink', 'archiveTitle', safeLang);
    applyNavLink('insightsLink', 'insightsTitle', safeLang);

    applyTabLabel('receivedTasksTab', 'receivedTasks', safeLang);
    applyTabLabel('delegatedTasksTab', 'delegatedTasks', safeLang);
    applyTabLabel('referenceTasksTab', 'referenceTasks', safeLang);
    applyTabLabel('allTasksTab', 'allTasks', safeLang);

    applyArchiveHeader(safeLang);
    applyStatusText('slackStatusLarge', 'slackStatusText', safeLang);
    applyStatusText('waStatusLarge', 'waStatusText', safeLang);
}
