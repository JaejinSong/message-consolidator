import { state, updateLang, updateTheme, updateStats } from './js/state.js';
import { updateUILanguage } from './js/i18n.js';
import { I18N_DATA } from './js/locales.js';
import { api } from './js/api.js';
import { renderer } from './js/renderer.js';
import { archive } from './js/archive.js';
import { modals } from './js/modals.js';
import { insights } from './js/insights.js';
import { events, EVENTS } from './js/events.js';
import { safeAsync, hasSessionHint } from './js/utils.js';
import { STATUS_STATES, POLLING_INTERVALS } from './js/constants.js';

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
    }, { triggerAuthOverlay: true }),
    onDeleteTask: safeAsync(async (id) => {
        await api.deleteTask(id);
        fetchMessages();
        if (archive.isVisible()) archive.fetch();
    }, { triggerAuthOverlay: true }),
    onShowOriginal: safeAsync(async (id) => {
        const data = await api.fetchOriginalMessage(id);
        if (data && data.original_text) {
            modals.showOriginalModal(data.original_text);
        }
    }, { triggerAuthOverlay: true })
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
    renderer.setScanLoading(true, state.currentLang);

    try {
        await api.triggerScan(state.currentLang);
        setTimeout(() => {
            fetchMessages();
            renderer.setScanLoading(false, state.currentLang);
        }, 5000);
    } catch (e) {
        console.error(e);
        renderer.setScanLoading(false, state.currentLang);
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
}, { triggerAuthOverlay: true });

/**
 * Handles streak freeze purchase.
 * Removed from window.buyStreakFreeze.
 */
const handleBuyStreakFreeze = safeAsync(async () => {
    if (!confirm('50 포인트를 사용하여 스트릭 보호권(❄️)을 구매하시겠습니까?')) return;
    try {
        await api.buyStreakFreeze();
        renderer.showToast('보호권이 구매되었습니다! 접속하지 못한 날 자동으로 사용되어 스트릭을 보호합니다.', 'success');
        fetchUserProfile();
    } catch (e) {
        renderer.showToast(e.message, 'error');
    }
}, { triggerAuthOverlay: true });

// --- Event Subscriptions ---

events.on(EVENTS.TASK_COMPLETED, (data) => {
    const gData = data?.result?.gamification;

    if (gData) {
        if (gData.XPAdded > 0) {
            renderer.triggerXPAnimation();
        }
        if (gData.IsCritical || gData.ComboActive) {
            renderer.triggerConfetti('star');
        }

        if (gData.UnlockedAchievements && gData.UnlockedAchievements.length > 0) {
            const lang = state.currentLang || 'ko';
            const i18n = I18N_DATA[lang];

            gData.UnlockedAchievements.forEach(ach => {
                renderer.triggerConfetti('star');

                const localizedName = i18n.achievements?.[ach.name]?.name || ach.name;
                const msg = lang === 'ko' ? `🏆 [${localizedName}] 배지를 획득했습니다!` : `🏆 Badge Unlocked: [${localizedName}]`;
                setTimeout(() => renderer.showToast(msg, 'success'), 300); // 폭죽과 겹치지 않게 살짝 딜레이
            });
        }
    } else {
        const rand = Math.random();
        if (rand < 0.05) { // 5% 확률로 기본 폭죽
            renderer.triggerConfetti('classic');
        } else if (rand < 0.08) { // 3% 확률로 별 모양
            renderer.triggerConfetti('star');
        } else if (rand < 0.10) { // 2% 확률로 눈송이
            renderer.triggerConfetti('snow');
        }
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
    renderer.setTheme(state.currentTheme);
    renderer.bindThemeToggle((isLight) => {
        updateTheme(isLight ? 'light' : 'dark');
    }, state.currentTheme === 'light');
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
            if (!view) return; // Scan 버튼(수동 액션)일 경우 뷰 전환 무시
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
    renderer.bindGetQRBtn(async () => {
        renderer.updateWhatsAppQR('generating', null, state.currentLang);
        try {
            const data = await api.getWhatsAppQR();
            if (data.qr) {
                renderer.updateWhatsAppQR('show', data.qr, state.currentLang);
                const poll = setInterval(async () => {
                    await checkWhatsAppStatus();
                    if (state.waConnected) {
                        clearInterval(poll);
                        renderer.updateWhatsAppQR('success', null, state.currentLang);
                    }
                }, 3001);
            }
        } catch (e) {
            renderer.updateWhatsAppQR('error', e.message, state.currentLang);
        }
    });

    renderer.bindScanBtn(triggerScan);

    renderer.bindGmailStatus(() => {
        if (!state.gmailConnected) {
            window.location.href = '/auth/gmail/connect';
        }
    });

    renderer.bindGlobalClicks({
        onBuyFreeze: handleBuyStreakFreeze
    });
};

/**
 * Initializes background polling.
 */
const initPolling = () => {
    setInterval(fetchMessages, POLLING_INTERVALS.MESSAGES);
    setInterval(checkWhatsAppStatus, POLLING_INTERVALS.WHATSAPP);
    setInterval(checkSlackStatus, POLLING_INTERVALS.SLACK);
    setInterval(checkGmailStatus, POLLING_INTERVALS.GMAIL);
    setInterval(() => modals.fetchTokenUsage(), POLLING_INTERVALS.TOKEN_USAGE);
};

/**
 * Main application initialization.
 */
const initApp = () => {
    console.log("Initializing Modular App...");

    if (!hasSessionHint()) {
        console.warn("No session hint found. Triggering login overlay.");
        const overlay = document.getElementById('loginOverlay');
        if (overlay) {
            overlay.classList.remove('hidden');
            overlay.style.display = 'flex';
        }
        // Do not proceed with initialization if no session hint
        return;
    }

    // Explicitly hide overlay if session hint is present
    const overlay = document.getElementById('loginOverlay');
    if (overlay) {
        overlay.classList.add('hidden');
        overlay.style.display = 'none';
    }

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
    insights.init?.();

    fetchUserProfile();
    checkWhatsAppStatus();
    checkSlackStatus();
    checkGmailStatus();
    modals.fetchTokenUsage();

    initPolling();
};

document.addEventListener('DOMContentLoaded', initApp);
