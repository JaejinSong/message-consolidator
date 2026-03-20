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
    const updateTab = (tabSelector, textKey, countId) => {
        const tab = document.querySelector(`[data-tab="${tabSelector}"]`);
        if (tab) {
            const countVal = document.getElementById(countId)?.textContent || '0';
            tab.innerHTML = `${data[textKey]} <span class="badge count" id="${countId}">${countVal}</span>`;
        }
    };
    updateTab('myTasksTab', 'myTasks', 'myCount');
    updateTab('otherTasksTab', 'otherTasks', 'otherCount');
    updateTab('allTasksTab', 'allTasks', 'allCount');

    // 아카이브 타이틀 뱃지 유지 처리
    const archiveTitle = document.querySelector('#archiveSection h2');
    if (archiveTitle) {
        const archiveCount = document.getElementById('archiveCount');
        const countText = archiveCount ? archiveCount.outerHTML : '';
        archiveTitle.innerHTML = `${data.archiveTitle} ${countText}`;
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
