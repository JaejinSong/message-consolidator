import '../static/style.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import 'pretendard/dist/web/static/pretendard.css';
import { state, updateLang, updateTheme, updateStats, updateMessages } from './state.ts';
import { updateUILanguage } from './i18n.js';
import { I18N_DATA } from './locales.js';
import { api } from './api.js';
import { 
    renderMessages, 
    renderEmptyGrid, 
    updateUserProfile, 
    updateWhatsAppStatus, 
    updateGmailStatus, 
    initMessageGridEvents,
    setScanLoading,
    showToast,
    updateWhatsAppQR,
    showWaModal,
    showGmailModal,
    updateQRTimer,
    updateSlackStatus,
    setTheme,
    triggerXPAnimation,
    triggerConfetti,
    bindGetQRBtn,
    bindScanBtn,
    bindWhatsAppStatus,
    bindGmailStatus,
    bindGlobalClicks,
    bindThemeToggle
} from './renderer.ts';
import { Message, MessageHandlers, ServiceHandlers, I18nDictionary, UserProfile, CategorizedMessages } from './types.ts';
import { archive } from './archive.js';
import { modals } from './modals.ts';
import { insights } from './insights.ts';
import { events, EVENTS } from './events.js';
import { safeAsync, hasSessionHint, setupTabs } from './utils.ts';
import { STATUS_STATES, POLLING_INTERVALS } from './constants.ts';
import { authService } from './services/authService.ts';

/**
 * @file app.ts
 * @description Main application entry point and coordinator.
 */

/**
 * Handlers for renderer actions.
 */
const handlers: ServiceHandlers = {
    onToggleDone: safeAsync(async (id: string, done: boolean) => {
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
    onDeleteTask: safeAsync(async (id: string) => {
        await api.deleteTask(id);
        fetchMessages();
        if (archive.isVisible()) archive.fetch();
    }, { triggerAuthOverlay: true }),
    onShowOriginal: safeAsync(async (id: string) => {
        const data = await api.fetchOriginalMessage(id);
        const lang = state.currentLang || 'ko';
        const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
        const msg = (data && data.original_text) ? data.original_text : i18n.originalNotAvailable;
        modals.showOriginalModal(msg!);
    }, { triggerAuthOverlay: true }),
    onMapAlias: (name: string) => {
        window.dispatchEvent(new CustomEvent('openAliasMapping', {
            detail: { name }
        }));
    },
    onWhatsAppLogout: safeAsync(async () => {
        const lang = state.currentLang || 'ko';
        const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
        if (!confirm(i18n.logoutConfirm!)) return;
        await api.logoutWhatsApp();
        showToast(lang === 'ko' ? '로그아웃 되었습니다.' : 'Logged out successfully.', 'success');
        checkWhatsAppStatus();
    }, { triggerAuthOverlay: true }),
    onWhatsAppRelink: safeAsync(async () => {
        updateWhatsAppQR('generating', null, state.currentLang);
        showWaModal();
        // waQRSection과 waConnectedSection은 renderer.updateServiceStatusUI에서 처리되지만,
        // 여기서는 강제로 QR 섹션을 보여줘야 함
        document.getElementById('waQRSection')?.classList.remove('hidden');
        document.getElementById('waConnectedSection')?.classList.add('hidden');

        await refreshWhatsAppQR();
    }, { triggerAuthOverlay: true }),
    onGmailDisconnect: safeAsync(async () => {
        const lang = state.currentLang || 'ko';
        const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
        if (!confirm(i18n.disconnectConfirm!)) return;
        const success = await authService.disconnectGmail();
        if (success) {
            showToast(lang === 'ko' ? '연동이 해제되었습니다.' : 'Disconnected successfully.', 'success');
            checkGmailStatus();
            document.getElementById('gmailModal')?.classList.add('hidden');
        } else {
            showToast(lang === 'ko' ? '연동 해제 실패' : 'Failed to disconnect.', 'error');
        }
    }, { triggerAuthOverlay: true }),
    onGmailConnect: () => {
        authService.connectGmail();
    }
};

/**
 * Fetches and renders messages.
 */
const fetchMessages = safeAsync(async () => {
    const data = await api.fetchMessages(state.currentLang);
    const categorized: CategorizedMessages = data.messages || data;
    if (data.user) {
        updateStats(data.user);
    }
    updateMessages(categorized);
    renderMessages(categorized, handlers);
});

/**
 * Checks Slack connection status.
 */
const checkSlackStatus = safeAsync(async () => {
    const data = await api.fetchSlackStatus();
    updateSlackStatus(data.status === STATUS_STATES.CONNECTED);
});

/**
 * Checks WhatsApp connection status.
 */
const checkWhatsAppStatus = safeAsync(async () => {
    const data = await api.fetchWhatsAppStatus();
    if (data) {
        state.waConnected = (data.status === STATUS_STATES.CONNECTED);
        updateWhatsAppStatus(data.status);
    }
});

/**
 * Checks Gmail connection status.
 */
const checkGmailStatus = safeAsync(async () => {
    const data = await authService.checkGmailStatus();
    state.gmailConnected = data.connected;
    updateGmailStatus(data.connected, data.email);
});

/**
 * Triggers a message scan.
 */
const triggerScan = async () => {
    setScanLoading(true);

    try {
        await api.triggerScan(state.currentLang);
        setTimeout(() => {
            fetchMessages();
            setScanLoading(false);
        }, 5000);
    } catch (e) {
        console.error(e);
        setScanLoading(false);
    }
};

/**
 * Fetches user profile and updates state.
 */
const fetchUserProfile = safeAsync(async () => {
    const data = await api.fetchUserProfile();
    state.userProfile = data;
    state.userAliases = (data.aliases || []) as string[];
    events.emit(EVENTS.USER_PROFILE_UPDATED, state.userProfile);
    fetchMessages();
}, { triggerAuthOverlay: true });

/**
 * Triggers batch translation for visible tasks in the current tab.
 */
const triggerBatchTranslation = safeAsync(async () => {
    const lang = state.currentLang;
    if (!lang || lang === 'en') return;

    const activeTab = document.querySelector('.tab-btn.active')?.getAttribute('data-tab') || 'myTasksTab';
    const gridId = activeTab.replace('Tab', 'List');
    const container = document.getElementById(gridId);
    if (!container) return;

    const cards = container.querySelectorAll('.c-message-card:not(.c-message-card--done)');
    if (cards.length === 0) return;

    const taskIds = Array.from(cards).map(card => parseInt((card as HTMLElement).dataset.id || '0', 10));

    let needsUpdate = false;
    taskIds.forEach(id => {
        const msg = state.messages.all.find(m => m.id === id);
        if (msg && !msg.translating) {
            msg.translating = true;
            msg.translationError = null;
            needsUpdate = true;
        }
    });

    if (needsUpdate) {
        renderMessages(state.messages, handlers);
    }

    try {
        const data = await api.translateTasksBatch(taskIds, lang);
        const results = data.results || [];

        results.forEach((res: any) => {
            const msg = state.messages.all.find(m => m.id === res.id);
            if (msg) {
                msg.translating = false;
                if (res.success) {
                    msg.task = res.translated_text;
                    msg.translationError = null;
                } else {
                    msg.translationError = res.error;
                }
            }
        });
    } catch (e) {
        console.error("Batch translation failed:", e);
        taskIds.forEach(id => {
            const msg = state.messages.all.find(m => m.id === id);
            if (msg) {
                msg.translating = false;
                msg.translationError = "Service Unavailable";
            }
        });
    } finally {
        renderMessages(state.messages, handlers);
    }
});

/**
 * Handles streak freeze purchase.
 */
const handleBuyStreakFreeze = safeAsync(async () => {
    if (!confirm('50 포인트를 사용하여 스트릭 보호권(❄️)을 구매하시겠습니까?')) return;
    try {
        await api.buyStreakFreeze();
        showToast('보호권이 구매되었습니다! 접속하지 못한 날 자동으로 사용되어 스트릭을 보호합니다.', 'success');
        fetchUserProfile();
    } catch (e: any) {
        showToast(e.message || 'Error purchasing streak freeze', 'error');
    }
}, { triggerAuthOverlay: true });

// --- Event Subscriptions ---

events.on(EVENTS.TASK_COMPLETED, (data: { result: { gamification: any } }) => {
    const gData = data?.result?.gamification;

    if (gData) {
        if (gData.XPAdded > 0) {
            triggerXPAnimation();
        }
        if (gData.IsCritical || gData.ComboActive) {
            triggerConfetti('star');
        }

        if (gData.UnlockedAchievements && gData.UnlockedAchievements.length > 0) {
            const lang = state.currentLang || 'ko';
            const i18n = (I18N_DATA as I18nDictionary)[lang];

            gData.UnlockedAchievements.forEach((ach: { name: string }) => {
                triggerConfetti('star');

                const localizedName = i18n.achievements?.[ach.name]?.name || ach.name;
                const msg = lang === 'ko' ? `🏆 [${localizedName}] 배지를 획득했습니다!` : `🏆 Badge Unlocked: [${localizedName}]`;
                setTimeout(() => showToast(msg, 'success'), 300);
            });
        }
    } else {
        const rand = Math.random();
        if (rand < 0.05) {
            triggerConfetti('classic');
        } else if (rand < 0.08) {
            triggerConfetti('star');
        } else if (rand < 0.10) {
            triggerConfetti('snow');
        }
        triggerXPAnimation();
    }
    fetchUserProfile();
});

events.on(EVENTS.USER_PROFILE_UPDATED, (profile: UserProfile) => {
    updateUserProfile(profile);
});

/**
 * Initializes theme and theme toggle.
 */
const initTheme = () => {
    setTheme(state.currentTheme || 'dark');
    bindThemeToggle((isLight: boolean) => {
        const newTheme = isLight ? 'light' : 'dark';
        updateTheme(newTheme);
        setTheme(newTheme);
        events.emit(EVENTS.THEME_CHANGED, newTheme);
    });
};

/**
 * Initializes language selector.
 */
const initLanguageSelector = () => {
    const langSelect = document.getElementById('languageSelect') as HTMLSelectElement;
    if (langSelect) {
        langSelect.value = state.currentLang;
        langSelect.addEventListener('change', async (e: Event) => {
            const target = e.target as HTMLSelectElement;
            const lang = target.value;
            updateLang(lang);
            events.emit(EVENTS.LANGUAGE_CHANGED, lang);
            updateUILanguage(lang);
            try {
                await fetchMessages();
                await triggerBatchTranslation();
                if (archive.isVisible()) {
                    archive.fetch();
                }
            } finally {
            }
        });
    }
};

/**
 * Initializes navigation and view switching.
 */
const initNavigation = () => {
    const showView = (view: string) => {
        console.log(`[Navigation] Switching to: ${view}`);
        window.scrollTo(0, 0);
        const dashboardContent = document.getElementById('dashboardContent');
        const dashboardHeader = document.querySelector('.dashboard-header');
        const archiveSection = document.getElementById('archiveSection');
        const insightsSection = document.getElementById('insightsSection');
        const navTabs = document.querySelectorAll('.c-main-nav__item');

        [dashboardContent, dashboardHeader, archiveSection, insightsSection].forEach(el => {
            el?.classList.add('hidden');
        });

        if (view === 'archive') {
            archiveSection?.classList.remove('hidden');
            archive.onShow();
        } else if (view === 'insights') {
            insightsSection?.classList.remove('hidden');
            insights.onShow();
        } else {
            dashboardContent?.classList.remove('hidden');
            dashboardHeader?.classList.remove('hidden');
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
            if (!view) return;
            showView(view);
        });
    });

    const closeArchive = () => showView('dashboard');
    document.getElementById('closeArchiveBtn')?.addEventListener('click', closeArchive);
    document.getElementById('backToDashBtn')?.addEventListener('click', closeArchive);
};

let qrRefreshInterval: ReturnType<typeof setInterval> | null = null;
let qrTimerInterval: ReturnType<typeof setInterval> | null = null;
const QR_EXPIRY_SECONDS = 20;

/**
 * Starts automatic QR refresh and countdown timer.
 */
const startQRAutoRefresh = () => {
    stopQRAutoRefresh();

    let remaining = QR_EXPIRY_SECONDS;
    updateQRTimer(remaining, QR_EXPIRY_SECONDS);

    qrTimerInterval = setInterval(() => {
        remaining--;
        if (remaining < 0) remaining = 0;
        updateQRTimer(remaining, QR_EXPIRY_SECONDS);
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
            updateWhatsAppQR('show', data.qr, state.currentLang);
            startQRAutoRefresh();
        }
    } catch (e: any) {
        updateWhatsAppQR('error', e.message || 'Failed to fetch QR', state.currentLang);
        stopQRAutoRefresh();
    }
};

/**
 * Initializes static action buttons and global event delegation.
 */
const initActionButtons = () => {
    bindGetQRBtn(async () => {
        updateWhatsAppQR('generating', null, state.currentLang);
        await refreshWhatsAppQR();

        const pollStatus = setInterval(async () => {
            await checkWhatsAppStatus();
            if (state.waConnected) {
                clearInterval(pollStatus);
                stopQRAutoRefresh();
                updateWhatsAppQR('success', null, state.currentLang);
            }
        }, 3001);
    });


    bindWhatsAppStatus(() => {
        showWaModal();
    });

    bindGmailStatus(() => {
        showGmailModal();
    });

    // New Bindings for logout/disconnect/relink
    document.getElementById('waLogoutBtn')?.addEventListener('click', handlers.onWhatsAppLogout);
    document.getElementById('waRelinkBtn')?.addEventListener('click', handlers.onWhatsAppRelink);
    document.getElementById('gmailDisconnectBtn')?.addEventListener('click', handlers.onGmailDisconnect);
    document.getElementById('gmailConnectBtn')?.addEventListener('click', handlers.onGmailConnect);
    document.getElementById('closeGmailModalBtn')?.addEventListener('click', () => {
        const modal = document.getElementById('gmailModal');
        if (modal) {
            modal.classList.add('hidden');
            modal.style.display = 'none';
        }
    });
    document.getElementById('closeWaModalBtn')?.addEventListener('click', () => {
        const modal = document.getElementById('waModal');
        if (modal) {
            modal.classList.add('hidden');
            modal.style.display = 'none';
        }
        stopQRAutoRefresh();
    });

    bindGlobalClicks({
        onBuyFreeze: handleBuyStreakFreeze
    });
};

/**
/**
 * Initializes background polling.
 */
const initPolling = () => {
    setInterval(fetchMessages, POLLING_INTERVALS.MESSAGES);
    setInterval(checkWhatsAppStatus, POLLING_INTERVALS.WHATSAPP);
    setInterval(checkSlackStatus, POLLING_INTERVALS.SLACK);
    setInterval(checkGmailStatus, POLLING_INTERVALS.GMAIL);
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

    setupTabs('#dashboardContent .tab-btn', '#dashboardContent .c-tabs__panel', 'data-tab', 'active', async () => {
        await fetchMessages();
        await triggerBatchTranslation();
    });
    setupTabs('.c-settings__tab', '.c-settings__panel', 'data-settings-tab', 'c-settings__tab--active', (tabId: string) => {
        if (tabId === 'tokenUsageTab') {
            modals.fetchTokenUsage();
        }
    });
    setTimeout(() => (document.querySelector('[data-tab="myTasksTab"]') as HTMLElement)?.click(), 500);

    initNavigation();
    initActionButtons();
    
    // Initialize Event Delegation for all grids
    ['myTasksList', 'otherTasksList', 'waitingTasksList', 'allTasksList'].forEach(id => {
        initMessageGridEvents(id, handlers);
    });

    archive.init(fetchMessages);
    modals.init(fetchMessages);
    insights.init?.();

    fetchUserProfile();
    checkWhatsAppStatus();
    checkSlackStatus();
    checkGmailStatus();

    initPolling();
};

document.addEventListener('DOMContentLoaded', initApp);
