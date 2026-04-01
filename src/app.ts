import '../static/style.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import 'pretendard/dist/web/static/pretendard.css';
import { state, updateLang, updateTheme, updateStats, updateMessages } from './state.js';
import { updateUILanguage } from './i18n.js';
import { I18N_DATA } from './locales.js';
import { api } from './api.js';
import { renderer } from './renderer.js';
import { archive } from './archive.js';
import { modals } from './modals.ts';
import { insights } from './insights.js';
import { events, EVENTS } from './events.js';
import { safeAsync, hasSessionHint, setupTabs } from './utils.js';
import { STATUS_STATES, POLLING_INTERVALS } from './constants.js';
import { authService } from './services/authService.ts';

/**
 * @file app.ts
 * @description Main application entry point and coordinator.
 */

/**
 * Handlers for renderer actions.
 */
interface Handlers {
    onToggleDone: (id: string, done: boolean) => Promise<void>;
    onDeleteTask: (id: string) => Promise<void>;
    onShowOriginal: (id: string) => Promise<void>;
    onWhatsAppLogout: () => Promise<void>;
    onWhatsAppRelink: () => Promise<void>;
    onGmailDisconnect: () => Promise<void>;
    onGmailConnect: () => void;
}

const handlers: Handlers = {
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
        const lang = (state as any).currentLang || 'ko';
        const msg = (data && data.original_text) ? data.original_text : (I18N_DATA as any)[lang].originalNotAvailable;
        modals.showOriginalModal(msg);
    }, { triggerAuthOverlay: true }),
    onWhatsAppLogout: safeAsync(async () => {
        const lang = (state as any).currentLang || 'ko';
        if (!confirm((I18N_DATA as any)[lang].logoutConfirm)) return;
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
        const lang = (state as any).currentLang || 'ko';
        if (!confirm((I18N_DATA as any)[lang].disconnectConfirm)) return;
        const success = await authService.disconnectGmail();
        if (success) {
            renderer.showToast(lang === 'ko' ? '연동이 해제되었습니다.' : 'Disconnected successfully.', 'success');
            checkGmailStatus();
            document.getElementById('gmailModal')?.classList.add('hidden');
        } else {
            renderer.showToast(lang === 'ko' ? '연동 해제 실패' : 'Failed to disconnect.', 'error');
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
    const data = await api.fetchMessages((state as any).currentLang);
    const messages = data.messages || data;
    if (data.user) {
        updateStats(data.user);
    }
    updateMessages(messages);
    renderer.renderMessages(messages, handlers);
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
    const data = await authService.checkGmailStatus();
    state.gmailConnected = data.connected;
    renderer.updateGmailStatus(data.connected, data.email);
});

/**
 * Triggers a message scan.
 */
const triggerScan = async () => {
    renderer.setScanLoading(true);

    try {
        await api.triggerScan(state.currentLang);
        setTimeout(() => {
            fetchMessages();
            renderer.setScanLoading(false);
        }, 5000);
    } catch (e) {
        console.error(e);
        renderer.setScanLoading(false);
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
 * Triggers batch translation for visible tasks in the current tab.
 */
const triggerBatchTranslation = safeAsync(async () => {
    const lang = state.currentLang;
    if (!lang || lang === 'en') return;

    // Identify tasks in the currently active tab
    const activeTab = document.querySelector('.tab-btn.active')?.getAttribute('data-tab') || 'myTasksTab';
    const gridId = activeTab.replace('Tab', 'List');
    const container = document.getElementById(gridId);
    if (!container) return;

    const cards = container.querySelectorAll('.c-task-card:not(.c-task-card--done)');
    if (cards.length === 0) return;

    const taskIds = Array.from(cards).map(card => parseInt((card as HTMLElement).dataset.id || '0', 10));

    // Update state to show loading spinners on relevant cards
    let needsUpdate = false;
    taskIds.forEach(id => {
        const msg: any = (state.messages as any[] || []).find(m => m.id === id);
        if (msg && !msg.translating) {
            msg.translating = true;
            msg.translationError = null;
            needsUpdate = true;
        }
    });

    if (needsUpdate) {
        renderer.renderMessages(state.messages, handlers);
    }

    try {
        const data = await api.translateTasksBatch(taskIds, lang);
        const results = data.results || [];

        results.forEach((res: any) => {
            const msg: any = (state.messages as any[] || []).find(m => m.id === res.id);
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
            const msg: any = (state.messages as any[] || []).find(m => m.id === id);
            if (msg) {
                msg.translating = false;
                msg.translationError = "Service Unavailable";
            }
        });
    } finally {
        renderer.renderMessages(state.messages || [], handlers);
    }
});

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
    } catch (e: any) {
        renderer.showToast(e.message || 'Error purchasing streak freeze', 'error');
    }
}, { triggerAuthOverlay: true });

// --- Event Subscriptions ---

events.on(EVENTS.TASK_COMPLETED, (data: any) => {
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
            const i18n = (I18N_DATA as any)[lang];

            gData.UnlockedAchievements.forEach((ach: any) => {
                renderer.triggerConfetti('star');

                const localizedName = (i18n as any).achievements?.[ach.name]?.name || ach.name;
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

events.on(EVENTS.USER_PROFILE_UPDATED, (profile: any) => {
    renderer.updateUserProfile(profile);
});

/**
 * Initializes theme and theme toggle.
 */
const initTheme = () => {
    renderer.setTheme(state.currentTheme);
    (renderer as any).bindThemeToggle((isLight: boolean) => {
        const newTheme = isLight ? 'light' : 'dark';
        updateTheme(newTheme);
        events.emit(EVENTS.THEME_CHANGED, newTheme);
    }, (state as any).currentTheme === 'light');
};



/**
 * Initializes language selector.
 */
const initLanguageSelector = () => {
    const langSelect = document.getElementById('languageSelect') as HTMLSelectElement;
    if (langSelect) {
        langSelect.value = (state as any).currentLang;
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
                // No global loading overlay used for JIT
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
        window.scrollTo(0, 0); // Reset scroll on view switch
        const dashboardContent = document.getElementById('dashboardContent');
        const dashboardHeader = document.querySelector('.dashboard-header');
        const archiveSection = document.getElementById('archiveSection');
        const insightsSection = document.getElementById('insightsSection');
        const navTabs = document.querySelectorAll('.c-main-nav__item');

        // Hide all major functional blocks
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
            if (!view) return; // Scan 버튼(수동 액션)일 경우 뷰 전환 무시
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
    } catch (e: any) {
        renderer.updateWhatsAppQR('error', (e as any).message || 'Failed to fetch QR', (state as any).currentLang);
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

    renderer.bindGlobalClicks({
        onBuyFreeze: handleBuyStreakFreeze
    });
};

/**
 * Optimized Event Delegation for Task Cards and Actions.
 * Attached to the specific parent container #dashboardContent.
 */
const initTaskDelegation = () => {
    const dashboardContent = document.getElementById('dashboardContent');
    if (!dashboardContent) return;

    dashboardContent.addEventListener('click', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        
        // Mandatory Debug Log for "Clicked element"
        console.log('[DEBUG] Clicked element:', target);

        // Resolve target button using .closest() to handle nested icons/SVGs
        const viewOriginalBtn = target.closest('.view-original-btn');
        const deleteBtn = target.closest('.delete-btn');
        const toggleDoneBtn = target.closest('.toggle-done-btn');
        const mapAliasBtn = target.closest('.map-alias-btn');
        const settingsBtn = target.closest('#settingsBtn');

        // Mandatory Debug Log for "Resolved button"
        const resolvedBtn = viewOriginalBtn || deleteBtn || toggleDoneBtn || mapAliasBtn || settingsBtn;
        if (resolvedBtn) {
            console.log('[DEBUG] Resolved button:', resolvedBtn);
        }

        // Action Handling logic using strict if...else if
        if (viewOriginalBtn) {
            const card = viewOriginalBtn.closest('.c-task-card') as HTMLElement;
            if (card && card.dataset.id) {
                handlers.onShowOriginal(card.dataset.id);
            }
        } else if (deleteBtn) {
            const card = deleteBtn.closest('.c-task-card') as HTMLElement;
            if (card && card.dataset.id) {
                handlers.onDeleteTask(card.dataset.id);
            }
        } else if (toggleDoneBtn) {
            const card = toggleDoneBtn.closest('.c-task-card') as HTMLElement;
            if (card && card.dataset.id) {
                const isDone = card.classList.contains('c-task-card--done');
                handlers.onToggleDone(card.dataset.id, !isDone);
            }
        } else if (mapAliasBtn) {
            const btn = mapAliasBtn as HTMLElement;
            window.dispatchEvent(new CustomEvent('openAliasMapping', {
                detail: { name: btn.dataset.name }
            }));
        } else if (settingsBtn) {
            // Note: Settings modal trigger is also handled by ID listener in modals.ts
            // We include it here for logging oversight and to ensure it's isolated.
            console.log('[DEBUG] Settings button identified in dashboard delegation');
        }
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

    setupTabs('#dashboardContent .tab-btn', '#dashboardContent .c-tabs__panel', 'data-tab', 'active', async () => {
        await fetchMessages();
        await triggerBatchTranslation();
    });
    setupTabs('.c-settings__tab', '.c-settings__panel', 'data-settings-tab', 'c-settings__tab--active');
    setTimeout(() => (document.querySelector('[data-tab="myTasksTab"]') as HTMLElement)?.click(), 500);

    initNavigation();
    initActionButtons();
    initTaskDelegation(); // Added centralized delegation

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
