import { state, updateLang, updateTheme, updateStats } from './js/state.js';
import { updateUILanguage } from './js/i18n.js';
import { I18N_DATA } from './js/locales.js';
import { api } from './js/api.js';
import { renderer } from './js/renderer.js';
import { archive } from './js/archive.js';
import { modals } from './js/modals.js';
import { insights } from './js/insights.js';
import { events, EVENTS } from './js/events.js';
import { safeAsync } from './js/utils.js';
import { STATUS_STATES } from './js/constants.js';

/**
 * @file app.js
 * @description Main application entry point and coordinator.
 */

/**
 * Handlers for renderer actions.
 */
const handlers = {
    onToggleDone: safeAsync(async (id, done) => {
        const result = await api.toggleDone(id, done);
        if (result.user) {
            updateStats(result.user);
        }
        if (done) {
            events.emit(EVENTS.TASK_COMPLETED, { id, result });
        } else {
            fetchMessages();
        }
    }),
    onDeleteTask: safeAsync(async (id) => {
        await api.deleteTask(id);
        fetchMessages();
        if (archive.isVisible()) archive.fetch();
    }),
    onShowOriginal: safeAsync(async (id) => {
        const data = await api.fetchOriginalMessage(id);
        if (data && data.original_text) {
            modals.showOriginalModal(data.original_text);
        }
    })
};

/**
 * Fetches and renders messages.
 */
const fetchMessages = safeAsync(async () => {
    const data = await api.fetchMessages(state.currentLang);
    if (data.user) {
        updateStats(data.user);
    }
    renderer.renderMessages(data.messages || data, handlers);
});

/**
 * Checks Slack connection status.
 */
const checkSlackStatus = safeAsync(async () => {
    const data = await api.fetchSlackStatus();
    renderer.updateSlackStatus(data.status === STATUS_STATES.CONNECTED);
});

/**
 * Checks WhatsApp connection status.
 */
const checkWhatsAppStatus = safeAsync(async () => {
    const data = await api.fetchWhatsAppStatus();
    if (data) {
        renderer.updateWhatsAppStatus(data.status);
    }
});

/**
 * Checks Gmail connection status.
 */
const checkGmailStatus = safeAsync(async () => {
    const data = await api.fetchGmailStatus();
    renderer.updateGmailStatus(data.connected);
});

/**
 * Triggers a message scan.
 */
const triggerScan = async () => {
    const btn = document.getElementById('scanBtn');
    const scanBtnText = document.getElementById('scanBtnText');
    const loading = document.getElementById('loading');
    const i18n = I18N_DATA[state.currentLang];

    if (btn) btn.disabled = true;
    if (scanBtnText) scanBtnText.textContent = '...';
    if (loading) loading.classList.remove('hidden');

    try {
        await api.triggerScan(state.currentLang);
        setTimeout(() => {
            fetchMessages();
            if (btn) btn.disabled = false;
            if (scanBtnText) scanBtnText.textContent = i18n.scanBtnText || 'SCAN';
            if (loading) loading.classList.add('hidden');
        }, 5000);
    } catch (e) {
        console.error(e);
        if (btn) btn.disabled = false;
        if (scanBtnText) scanBtnText.textContent = i18n.scanBtnText || 'SCAN';
        if (loading) loading.classList.add('hidden');
    }
};

/**
 * Fetches user profile and updates state.
 */
const fetchUserProfile = safeAsync(async () => {
    const data = await api.fetchUserProfile();
    console.log('[DEBUG] User Profile Data:', data);
    state.userProfile = data;
    state.userAliases = data.aliases || [];
    events.emit(EVENTS.USER_PROFILE_UPDATED, state.userProfile);
    fetchMessages();
});

/**
 * Handles streak freeze purchase.
 * Removed from window.buyStreakFreeze.
 */
const handleBuyStreakFreeze = safeAsync(async () => {
    if (!confirm('50 포인트를 사용하여 스트릭 보호권(❄️)을 구매하시겠습니까?')) return;
    try {
        await api.buyStreakFreeze();
        alert('보호권이 구매되었습니다! 접속하지 못한 날 자동으로 사용되어 스트릭을 보호합니다.');
        fetchUserProfile();
    } catch (e) {
        alert(e.message);
    }
});

// --- Event Subscriptions ---

events.on(EVENTS.TASK_COMPLETED, (data) => {
    const gData = data?.result?.gamification;

    if (gData) {
        if (gData.XPAdded > 0) {
            renderer.triggerXPAnimation();
        }
        if (gData.IsCritical || gData.ComboActive) {
            renderer.triggerConfetti();
        }

        if (gData.UnlockedAchievements && gData.UnlockedAchievements.length > 0) {
            gData.UnlockedAchievements.forEach(ach => {
                renderer.triggerConfetti();
                alert(`🎉 새로운 업적 달성!\n[${ach.icon} ${ach.name}]\n${ach.description}`);
            });
        }
    } else {
        renderer.triggerConfetti();
        renderer.triggerXPAnimation();
    }
    fetchUserProfile();
});

events.on(EVENTS.USER_PROFILE_UPDATED, (profile) => {
    renderer.updateUserProfile(profile);
});

/**
 * Initializes theme and theme toggle.
 */
const initTheme = () => {
    if (state.currentTheme === 'light') {
        document.body.classList.add('light-theme');
    }

    const themeToggleBtn = document.getElementById('themeToggleBtn');
    if (themeToggleBtn) {
        const updateThemeIcon = (isLight) => {
            themeToggleBtn.innerHTML = isLight
                ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 20px; height: 20px;"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>`
                : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 20px; height: 20px;"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>`;
        };

        updateThemeIcon(state.currentTheme === 'light');

        themeToggleBtn.addEventListener('click', () => {
            const isLight = document.body.classList.toggle('light-theme');
            updateTheme(isLight ? 'light' : 'dark');
            updateThemeIcon(isLight);
        });
    }
};

/**
 * Sets up tab switching logic.
 */
const setupTabs = (btnSelector, contentSelector, attrName) => {
    const tabs = document.querySelectorAll(btnSelector);
    const contents = document.querySelectorAll(contentSelector);

    const switchTab = (tabId) => {
        tabs.forEach(b => b.classList.toggle('active', b.getAttribute(attrName) === tabId));
        contents.forEach(c => c.classList.toggle('active', c.id === tabId));
    };

    tabs.forEach(btn => {
        btn.addEventListener('click', () => {
            switchTab(btn.getAttribute(attrName));
            // 대시보드 탭 변경 시 즉시 화면 리스트 갱신
            if (btnSelector === '.tab-btn:not(.settings-tab-btn)') {
                fetchMessages();
            }
        });
    });
};

/**
 * Initializes language selector.
 */
const initLanguageSelector = () => {
    const langSelect = document.getElementById('languageSelect');
    if (langSelect) {
        langSelect.value = state.currentLang;
        langSelect.addEventListener('change', async (e) => {
            const lang = e.target.value;
            updateLang(lang);
            updateUILanguage(lang);
            const loading = document.getElementById('loading');
            loading.classList.remove('hidden');
            try {
                await api.translateTasks(lang);
                await fetchMessages();
                if (archive.isVisible()) {
                    archive.fetch();
                }
            } finally {
                loading.classList.add('hidden');
            }
        });
    }
};

/**
 * Initializes navigation and view switching.
 */
const initNavigation = () => {
    const showView = (view) => {
        const dashboardTabs = document.querySelector('.tabs-container');
        const dashboardHeader = document.querySelector('.dashboard-header');
        const archiveSection = document.getElementById('archiveSection');
        const insightsSection = document.getElementById('insightsSection');
        const navTabs = document.querySelectorAll('.nav-tab');

        if (view === 'archive') {
            dashboardTabs?.classList.add('hidden');
            dashboardHeader?.classList.add('hidden');
            insightsSection?.classList.add('hidden');
            archiveSection?.classList.remove('hidden');
            archive.onShow();
        } else if (view === 'insights') {
            dashboardTabs?.classList.add('hidden');
            dashboardHeader?.classList.add('hidden');
            archiveSection?.classList.add('hidden');
            insightsSection?.classList.remove('hidden');
            insights.onShow();
        } else {
            dashboardTabs?.classList.remove('hidden');
            dashboardHeader?.classList.remove('hidden');
            archiveSection?.classList.add('hidden');
            insightsSection?.classList.add('hidden');
            fetchMessages();
        }

        navTabs.forEach(tab => {
            const isMatch = tab.getAttribute('data-view') === view;
            tab.classList.toggle('active', isMatch);
        });
    };

    document.querySelectorAll('.nav-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            const view = tab.getAttribute('data-view');
            showView(view);
        });
    });

    const closeArchive = () => showView('dashboard');
    document.getElementById('closeArchiveBtn')?.addEventListener('click', closeArchive);
    document.getElementById('backToDashBtn')?.addEventListener('click', closeArchive);
};

/**
 * Initializes static action buttons and global event delegation.
 */
const initActionButtons = () => {
    document.getElementById('getQRBtn')?.addEventListener('click', async () => {
        const btn = document.getElementById('getQRBtn');
        const img = document.getElementById('waQRImg');
        const placeholder = document.getElementById('qrPlaceholder');
        const i18n = I18N_DATA[state.currentLang];

        btn.disabled = true;
        placeholder.textContent = i18n.generating;
        placeholder.classList.remove('hidden');
        img.classList.add('hidden');

        try {
            const data = await api.getWhatsAppQR();
            if (data.qr) {
                img.src = `data:image/png;base64,${data.qr}`;
                img.classList.remove('hidden');
                placeholder.classList.add('hidden');

                const poll = setInterval(async () => {
                    await checkWhatsAppStatus();
                    if (state.waConnected) {
                        clearInterval(poll);
                        btn.disabled = false;
                    }
                }, 3001);
            }
        } catch (e) {
            placeholder.textContent = i18n.error || 'Error';
            alert(i18n.qrError + e.message);
            btn.disabled = false;
        }
    });

    document.getElementById('scanBtn')?.addEventListener('click', triggerScan);

    document.getElementById('gmailStatusLarge')?.addEventListener('click', () => {
        if (!state.gmailConnected) {
            window.location.href = '/auth/gmail/connect';
        }
    });

    // Global Event Delegation for dynamic elements
    document.body.addEventListener('click', (e) => {
        // Handle Streak Freeze purchase
        if (e.target && e.target.closest('#buyFreezeBtn')) {
            handleBuyStreakFreeze();
        }
    });
};

/**
 * Initializes background polling.
 */
const initPolling = () => {
    setInterval(fetchMessages, 29009);
    setInterval(checkWhatsAppStatus, 31013);
    setInterval(checkSlackStatus, 41017);
    setInterval(checkGmailStatus, 61001);
    setInterval(() => modals.fetchTokenUsage(), 60000);
};

/**
 * Main application initialization.
 */
const initApp = () => {
    console.log("Initializing Modular App...");

    updateUILanguage(state.currentLang);
    initTheme();
    initLanguageSelector();

    setupTabs('.tab-btn:not(.settings-tab-btn)', '.tab-content:not(.settings-tab-content)', 'data-tab');
    setupTabs('.settings-tab-btn', '.settings-tab-content', 'data-settings-tab');
    setTimeout(() => document.querySelector('[data-tab="myTasksTab"]')?.click(), 500);

    initNavigation();
    initActionButtons();

    archive.init(fetchMessages);
    modals.init(fetchMessages);
    insights.init();

    fetchUserProfile();
    checkWhatsAppStatus();
    checkSlackStatus();
    checkGmailStatus();
    modals.fetchTokenUsage();

    initPolling();
};

document.addEventListener('DOMContentLoaded', initApp);
