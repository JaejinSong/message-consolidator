import { state, updateLang, updateTheme, updateStats } from './js/state.js';
import { updateUILanguage } from './js/i18n.js';
import { I18N_DATA } from './js/locales.js';
import { api } from './js/api.js';
import { renderer } from './js/renderer.js';
import { archive } from './js/archive.js';
import { modals } from './js/modals.js';
import { insights } from './js/insights.js';
import { events, EVENTS } from './js/events.js';
import { safeAsync, hasSessionHint, setupTabs } from './js/utils.js';
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
        const lang = state.currentLang || 'ko';
        const msg = (data && data.original_text) ? data.original_text : I18N_DATA[lang].originalNotAvailable;
        modals.showOriginalModal(msg);
    }, { triggerAuthOverlay: true }),
    onWhatsAppLogout: safeAsync(async () => {
        const lang = state.currentLang || 'ko';
        if (!confirm(I18N_DATA[lang].logoutConfirm)) return;
        await api.logoutWhatsApp();
        renderer.showToast(lang === 'ko' ? '로그아웃 되었습니다.' : 'Logged out successfully.', 'success');
        checkWhatsAppStatus();
    }, { triggerAuthOverlay: true }),
    onWhatsAppRelink: safeAsync(async () => {
        renderer.updateWhatsAppQR('generating', null, state.currentLang);
        renderer.showWaModal();
        // waQRSection과 waConnectedSection은 renderer.updateServiceStatusUI에서 처리되지만,
        // 여기서는 강제로 QR 섹션을 보여줘야 함
        document.getElementById('waQRSection')?.classList.remove('hidden');
        document.getElementById('waConnectedSection')?.classList.add('hidden');

        await refreshWhatsAppQR();
    }, { triggerAuthOverlay: true }),
    onGmailDisconnect: safeAsync(async () => {
        const lang = state.currentLang || 'ko';
        if (!confirm(I18N_DATA[lang].disconnectConfirm)) return;
        await api.disconnectGmail();
        renderer.showToast(lang === 'ko' ? '연동이 해제되었습니다.' : 'Disconnected successfully.', 'success');
        checkGmailStatus();
        document.getElementById('gmailModal')?.classList.add('hidden');
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
        state.waConnected = (data.status === STATUS_STATES.CONNECTED);
        renderer.updateWhatsAppStatus(data.status);
    }
});

/**
 * Checks Gmail connection status.
 */
const checkGmailStatus = safeAsync(async () => {
    const data = await api.fetchGmailStatus();
    state.gmailConnected = data.connected;
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
        const navTabs = document.querySelectorAll('.c-main-nav__item');

        const isArchive = view === 'archive';
        const isInsights = view === 'insights';
        const isDashboard = !isArchive && !isInsights;

        dashboardTabs?.classList.toggle('hidden', !isDashboard);
        dashboardHeader?.classList.toggle('hidden', !isDashboard);
        archiveSection?.classList.toggle('hidden', !isArchive);
        insightsSection?.classList.toggle('hidden', !isInsights);

        if (isArchive) {
            archive.onShow();
        } else if (isInsights) {
            insights.onShow();
        } else {
            fetchMessages();
        }

        navTabs.forEach(tab => {
            const isMatch = tab.getAttribute('data-view') === view;
            tab.classList.toggle('c-main-nav__item--active', isMatch);
        });
    };

    document.querySelectorAll('.c-main-nav__item').forEach(tab => {
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

let qrRefreshInterval = null;
let qrTimerInterval = null;
const QR_EXPIRY_SECONDS = 20;

/**
 * Starts automatic QR refresh and countdown timer.
 */
const startQRAutoRefresh = () => {
    stopQRAutoRefresh(); // Clear existing

    let remaining = QR_EXPIRY_SECONDS;
    renderer.updateQRTimer(remaining, QR_EXPIRY_SECONDS);

    qrTimerInterval = setInterval(() => {
        remaining--;
        if (remaining < 0) remaining = 0;
        renderer.updateQRTimer(remaining, QR_EXPIRY_SECONDS);
    }, 1000);

    qrRefreshInterval = setInterval(async () => {
        if (state.waConnected) {
            stopQRAutoRefresh();
            return;
        }
        await refreshWhatsAppQR();
    }, QR_EXPIRY_SECONDS * 1000);
};

const stopQRAutoRefresh = () => {
    if (qrRefreshInterval) clearInterval(qrRefreshInterval);
    if (qrTimerInterval) clearInterval(qrTimerInterval);
};

const refreshWhatsAppQR = async () => {
    try {
        const data = await api.getWhatsAppQR();
        if (data.qr) {
            renderer.updateWhatsAppQR('show', data.qr, state.currentLang);
            startQRAutoRefresh(); // Reset timer on successful fetch
        }
    } catch (e) {
        renderer.updateWhatsAppQR('error', e.message, state.currentLang);
        stopQRAutoRefresh();
    }
};

/**
 * Initializes static action buttons and global event delegation.
 */
const initActionButtons = () => {
    renderer.bindGetQRBtn(async () => {
        renderer.updateWhatsAppQR('generating', null, state.currentLang);
        await refreshWhatsAppQR();

        const pollStatus = setInterval(async () => {
            await checkWhatsAppStatus();
            if (state.waConnected) {
                clearInterval(pollStatus);
                stopQRAutoRefresh();
                renderer.updateWhatsAppQR('success', null, state.currentLang);
            }
        }, 3001);
    });

    renderer.bindScanBtn(triggerScan);

    renderer.bindWhatsAppStatus(() => {
        renderer.showWaModal();
    });

    renderer.bindGmailStatus(() => {
        renderer.showGmailModal();
    });

    // New Bindings for logout/disconnect/relink
    document.getElementById('waLogoutBtn')?.addEventListener('click', handlers.onWhatsAppLogout);
    document.getElementById('waRelinkBtn')?.addEventListener('click', handlers.onWhatsAppRelink);
    document.getElementById('gmailDisconnectBtn')?.addEventListener('click', handlers.onGmailDisconnect);
    document.getElementById('closeGmailModalBtn')?.addEventListener('click', () => {
        document.getElementById('gmailModal')?.classList.add('hidden');
        document.getElementById('gmailModal').style.display = 'none';
    });
    document.getElementById('closeWaModalBtn')?.addEventListener('click', () => {
        const modal = document.getElementById('waModal');
        if (modal) {
            modal.classList.add('hidden');
            modal.style.display = 'none';
        }
        stopQRAutoRefresh();
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

    setupTabs('#dashboardContent .tab-btn', '#dashboardContent .c-tabs__panel', 'data-tab', 'active', () => {
        fetchMessages();
    });
    setupTabs('.c-settings__tab', '.c-settings__panel', 'data-settings-tab', 'c-settings__tab--active');
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
