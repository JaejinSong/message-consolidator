import { state } from './state.js';
import { I18N_DATA } from './locales.js';

export const updateUILanguage = (lang) => {
    const data = I18N_DATA[lang];
    if (!data) return;

    // Update generic text
    document.querySelectorAll('[data-i18n]').forEach(el => {
        const key = el.getAttribute('data-i18n');
        if (I18N_DATA[lang] && I18N_DATA[lang][key]) {
            el.textContent = I18N_DATA[lang][key];
        }
    });

    // Update titles (tooltips)
    document.querySelectorAll('[data-i18n-title]').forEach(el => {
        const key = el.getAttribute('data-i18n-title');
        if (I18N_DATA[lang] && I18N_DATA[lang][key]) {
            el.title = I18N_DATA[lang][key];
        }
    });

    // Update placeholders (검색창 등)
    document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
        const key = el.getAttribute('data-i18n-placeholder');
        if (data[key]) {
            el.placeholder = data[key];
        }
    });

    // 3. 자식 요소(Badge) 유지가 필요한 특수 탭 처리
    const updateCountBadge = (tabSelector, textKey, countId) => {
        const tab = document.querySelector(`.tab-btn[data-tab="${tabSelector}"]`); 
        if (tab) {
            const labelEl = tab.querySelector(`[data-i18n="${textKey}"]`);
            if (labelEl && data[textKey]) {
                labelEl.textContent = data[textKey];
            }
            // Badge ID는 고유하므로 바로 접근 가능 (innerHTML 덮어쓰기 방지)
            const badgeEl = document.getElementById(countId);
            if (badgeEl) {
                // 기존 숫자를 유지하거나 필요시 갱신 (이미 renderer.js에서 처리됨)
            }
        }
    };

    
    // Main Nav Buttons (Dashboard, Archive, Insights)
    const updateMainNavLink = (tabSelector, textKey) => {
        const tab = document.querySelector(`.c-main-nav__item[data-tab="${tabSelector}"]`); // Specific to main nav
        if (tab) {
            tab.textContent = data[textKey] || tab.textContent;
        }
    };

    updateMainNavLink('myTasksTab', 'dashboardTitle');
    updateMainNavLink('archiveLink', 'archiveTitle');
    updateMainNavLink('insightsLink', 'insightsTitle');

    // Category Tabs (My Tasks, Other Tasks, etc.)
    updateCountBadge('myTasksTab', 'myTasks', 'myCount');
    updateCountBadge('otherTasksTab', 'otherTasks', 'otherCount');
    updateCountBadge('waitingTasksTab', 'waitingTasks', 'waitingCount');
    updateCountBadge('allTasksTab', 'allTasks', 'allCount');

    // 아카이브 섹션 헤더 타이틀 (h2)
    const archiveHeaderTitle = document.querySelector('#archiveSection h2');
    if (archiveHeaderTitle) {
        const archiveCount = document.getElementById('archiveCount');
        const countText = archiveCount ? archiveCount.outerHTML : '';
        archiveHeaderTitle.innerHTML = `${data.archiveTitle} ${countText}`;
    }

    // 4. 동적 텍스트 (ON / OFF 상태) 업데이트 처리
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
