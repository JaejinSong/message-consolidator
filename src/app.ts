import '../static/style.css';
import '@fortawesome/fontawesome-free/css/all.min.css';
import { state, updateLang, updateTheme, updateStats, updateMessages, setTaskSelection, clearTaskSelection, deleteTaskFromState, updateTaskStatusInState, updateSubtaskStateInState, getTaskById, upsertItem } from './state';
import { renderUILanguage } from './renderers/i18n-renderer';
import { I18N_DATA } from './locales';
import { api } from './api';
import { 
    renderMessages, 
    updateUserProfile, 
    updateWhatsAppStatus, 
    updateGmailStatus, 
    initMessageGridEvents,
    showToast,
    updateWhatsAppQR,
    showWaModal,
    showGmailModal,
    updateQRTimer,
    updateSlackStatus,
    setTheme,
    bindGetQRBtn,
    bindWhatsAppStatus,
    bindGmailStatus,
    bindGlobalClicks,
    bindThemeToggle,
    removeTaskNode,
    updateTaskNodeStatus,
    updateSubtaskNodeStatus,
    getVisibleUntranslatedIds
} from './renderer';
import { I18nDictionary, ServiceHandlers, UserProfile, CategorizedMessages } from './types';
import { archive } from './archive';
import { modals } from './modals';
import { insights } from './insights';
import { events, EVENTS } from './events';
import { safeAsync, hasSessionHint, setupTabs, escapeHTML } from './utils';
import { STATUS_STATES, POLLING_INTERVALS } from './constants';
import { authService } from './services/authService';

let lastMessageDataHash = '';
const activeTimers: any[] = [];

/**
 * @file app.ts
 * @description Main application entry point and coordinator.
 */

/**
 * Handlers for renderer actions.
 */
const handlers: ServiceHandlers = {
    onToggleDone: safeAsync(async (idStr: string, done: boolean) => {
        const id = Number(idStr);
        const oldTask = getTaskById(id);
        if (!oldTask) return;

        // 1. Optimistic Update
        updateTaskStatusInState(id, done);
        updateTaskNodeStatus(id, done);

        try {
            const result = await api.toggleDone(idStr, done);
            if (result && result.user) updateStats(result.user);
            if (done) {
                events.emit(EVENTS.TASK_COMPLETED, { id: idStr, result });
            }
        } catch (e: any) {
            // 2. Rollback on Failure
            showToast(state.currentLang === 'ko' ? '상태 업데이트 실패' : 'Failed to update status', 'error');
            updateTaskStatusInState(id, !done);
            updateTaskNodeStatus(id, !done);
        }
    }, { triggerAuthOverlay: true }),

    onDeleteTask: safeAsync(async (idStr: string) => {
        const id = Number(idStr);
        const oldTask = getTaskById(id);
        if (!oldTask) return;

        // 1. Optimistic Removal
        deleteTaskFromState(id);
        removeTaskNode(id);

        try {
            const result = await api.deleteTask(idStr);
            if (result && result.user) updateStats(result.user);
            if (archive.isVisible()) archive.fetch();
        } catch (e: any) {
            // 2. Rollback on Failure
            showToast(state.currentLang === 'ko' ? '삭제 실패' : 'Delete failed', 'error');
            // Restore in state
            state.messages.inbox = upsertItem(state.messages.inbox, oldTask);
            // Since it was removed from DOM, we need to trigger a re-render or re-insert
            // Full re-render is safest for rollback
            renderMessages(state.messages);
        }
    }, { triggerAuthOverlay: true }),
    onShowOriginal: safeAsync(async (id: string) => {
        const data = await api.fetchOriginalMessage(id);
        const lang = state.currentLang || 'en';
        const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['en'];
        const msg = (data && data.original_text) ? data.original_text : i18n.originalNotAvailable;
        modals.showOriginalModal(msg!);
    }, { triggerAuthOverlay: true }),
    onMapAlias: (name: string) => {
        window.dispatchEvent(new CustomEvent('openAliasMapping', {
            detail: { name }
        }));
    },
    onWhatsAppLogout: safeAsync(async () => {
        const lang = state.currentLang || 'en';
        const i18n = (I18N_DATA as I18nDictionary)[lang] ?? (I18N_DATA as I18nDictionary)['en'];
        if (!confirm(i18n.logoutConfirm!)) return;
        await api.logoutWhatsApp();
        showToast(lang === 'ko' ? '로그아웃 되었습니다.' : 'Logged out successfully.', 'success');
        checkAllStatus(true);
    }, { triggerAuthOverlay: true }),
    onWhatsAppRelink: safeAsync(async () => {
        updateWhatsAppQR('generating', null, state.currentLang);
        showWaModal();
        document.getElementById('waQRSection')?.classList.remove('hidden');
        document.getElementById('waConnectedSection')?.classList.add('hidden');

        await refreshWhatsAppQR();
    }, { triggerAuthOverlay: true }),
    onGmailDisconnect: safeAsync(async () => {
        const lang = state.currentLang || 'en';
        const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['en'];
        if (!confirm(i18n.disconnectConfirm!)) return;
        const success = await authService.disconnectGmail();
        if (success) {
            showToast(lang === 'ko' ? '연동이 해제되었습니다.' : 'Disconnected successfully.', 'success');
            checkAllStatus(true);
            document.getElementById('gmailModal')?.classList.add('hidden');
        } else {
            showToast(lang === 'ko' ? '연동 해제 실패' : 'Failed to disconnect.', 'error');
        }
    }, { triggerAuthOverlay: true }),
    onGmailConnect: () => {
        authService.connectGmail();
    },
    onSelectTask: (id: number, selected: boolean) => {
        console.log(`[DEBUG] app.ts - onSelectTask called with id: ${id}, selected: ${selected}`);
        setTaskSelection(id, selected);
        updateMergeBar();
    },
    onToggleSubtask: safeAsync(async (taskIdStr: string, subtaskIndex: number, done: boolean) => {
        const taskId = Number(taskIdStr);
        
        // 1. Optimistic Update
        updateSubtaskStateInState(taskId, subtaskIndex, done);
        updateSubtaskNodeStatus(taskId, subtaskIndex, done);

        try {
            await api.toggleSubtask(taskIdStr, subtaskIndex, done);
        } catch (e: any) {
            showToast(state.currentLang === 'ko' ? '서브태스크 업데이트 실패' : 'Failed to update subtask', 'error');
            // Rollback
        }
    }, { triggerAuthOverlay: true })
};

/**
 * Updates the visibility and count of the Merge Selection Bar.
 */
const updateMergeBar = () => {
    const bar = document.getElementById('mergeSelectionBar');
    const countEl = document.getElementById('mergeBarCount');
    const count = state.selectedTaskIds.size;
    
    if (count >= 2) {
        bar?.classList.remove('hidden');
        if (countEl) countEl.textContent = count.toString();
    } else {
        bar?.classList.add('hidden');
    }
};

/**
 * Fetches and renders messages with robust locking.
 */
const fetchMessages = safeAsync(async (bypassVisibility: boolean = false) => {
    // Robust Lock: Prevent concurrent fetches even if visibility is bypassed
    if (state.isFetchingMessages || (!bypassVisibility && document.hidden)) return;
    
    state.isFetchingMessages = true;
    try {
        const data = await api.fetchMessages(state.currentLang);
        const categorized: CategorizedMessages = data.messages || data;
        
        // Simple change detection to avoid redundant triggers
        const currentHash = JSON.stringify(categorized);
        const hasChanged = currentHash !== lastMessageDataHash;
        lastMessageDataHash = currentHash;

        if (data.user) updateStats(data.user);
        updateMessages(categorized);
        renderMessages(categorized);
        
        // Auto-translate if visibility bypassed (tab switch, manual sync) OR if new content found
        if (bypassVisibility || hasChanged) {
            triggerBatchTranslation();
        }
    } finally {
        state.isFetchingMessages = false;
    }
});

/**
 * Why: Triggers translation for visible untranslated items in a single batch.
 * Consistent with User Requirement: Execute during lang change and transitions.
 */
async function triggerBatchTranslation(): Promise<void> {
    const ids = getVisibleUntranslatedIds();
    if (ids.length === 0) return;

    const targetLang = state.currentLang || 'en';
    if (targetLang === 'en') return;

    // Mark as translating in state immediately to prevent duplicate triggers
    const all = [...state.messages.inbox, ...state.messages.pending];
    ids.forEach(id => {
        const m = all.find(item => item.id === id);
        if (m) m.is_translating = true;
    });

    try {
        await Promise.all(ids.map(id => api.requestTranslation(id, targetLang)));
    } catch (e) {
        console.error('[I18N] Batch translation failed', e);
    }
}

/**
 * Checks all status channels with robust locking.
 */
const checkAllStatus = safeAsync(async (bypassVisibility: boolean = false) => {
    if (state.isFetchingStatus || (!bypassVisibility && document.hidden)) return;
    
    state.isFetchingStatus = true;
    try {
        await Promise.allSettled([
            api.fetchSlackStatus().then(d => updateSlackStatus(d.status === STATUS_STATES.CONNECTED)),
            api.fetchWhatsAppStatus().then(d => {
                if (d) {
                    state.waConnected = (d.status === STATUS_STATES.CONNECTED);
                    updateWhatsAppStatus(d.status);
                }
            }),
            authService.checkGmailStatus().then(d => {
                state.gmailConnected = d.connected;
                updateGmailStatus(d.connected, d.email);
            })
        ]);
    } finally {
        state.isFetchingStatus = false;
    }
});

/**
 * Fetches user profile and updates state.
 */
const fetchUserProfile = safeAsync(async () => {
    if (state.isFetchingMessages) return;
    state.isFetchingMessages = true;
    try {
        const data = await api.fetchUserProfile();
        state.userProfile = data;
        state.userAliases = (data.aliases || []) as string[];
        events.emit(EVENTS.USER_PROFILE_UPDATED, state.userProfile);
    } finally {
        state.isFetchingMessages = false;
    }
    // Call fetchMessages AFTER releasing the lock to avoid blocking
    await fetchMessages(true);
}, { triggerAuthOverlay: true });



// --- Event Subscriptions ---

events.on(EVENTS.TASK_COMPLETED, () => {
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
            renderUILanguage(lang);
            try {
                await fetchMessages(true);
                if (archive.isVisible()) {
                    archive.fetch();
                }
            } catch (e) {
                console.error('[I18n] Language switch refresh failed', e);
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
            // Allow browser to perform layout before initializing charts
            requestAnimationFrame(() => {
                insights.onShow();
            });
        } else {
            dashboardContent?.classList.remove('hidden');
            dashboardHeader?.classList.remove('hidden');
            fetchMessages(true);
        }

        navTabs.forEach(tab => {
            const isMatch = tab.getAttribute('data-view') === view;
            tab.classList.toggle('c-main-nav__item--active', isMatch);
        });
    };

    // Event Delegation for Main Navigation
    const navContainer = document.querySelector('.c-main-nav');
    if (navContainer) {
        navContainer.addEventListener('click', (e) => {
            const target = (e.target as HTMLElement).closest('.c-main-nav__item') as HTMLElement;
            if (!target) return;
            
            const view = target.getAttribute('data-view');
            if (view) showView(view);
        });
    }

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
            await checkAllStatus(true);
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

    bindGlobalClicks({});

    // --- Merge Logic ---
    document.getElementById('mergeTasksBtn')?.addEventListener('click', () => {
        const ids = Array.from(state.selectedTaskIds).sort((a, b) => a - b);
        if (ids.length < 2) return;
        
        const lang = state.currentLang || 'en';
        const modal = document.getElementById('mergeConfirmModal');
        const desc = document.getElementById('mergeConfirmDesc');
        if (!modal || !desc) return;

        // Find titles from current messages state
        const allMsgs = [...state.messages.inbox, ...state.messages.pending];
        const getTitle = (id: number) => {
            const m = allMsgs.find(msg => msg.id === id);
            return m ? (m.task.length > 50 ? m.task.substring(0, 47) + '...' : m.task) : `#${id}`;
        };

        const destId = ids[0];
        const sourceIds = ids.slice(1);
        const destTitle = escapeHTML(getTitle(destId));
        const sourceTitles = sourceIds.map(id => `<strong>"${escapeHTML(getTitle(id))}"</strong>`).join(', ');

        const msgHtml = lang === 'ko'
            ? `${sourceTitles} 를 <br><span class="u-text-accent"><strong>"${destTitle}"</strong></span> (으)로 병합하시겠습니까?`
            : `${sourceTitles} will be merged into <br><span class="u-text-accent"><strong>"${destTitle}"</strong></span>. Proceed?`;

        desc.innerHTML = msgHtml;
        modal.classList.remove('hidden');

        const confirmBtn = document.getElementById('confirmMergeBtn');
        const handleConfirm = async () => {
            modal.classList.add('hidden');
            confirmBtn?.removeEventListener('click', handleConfirm);
            
            try {
                await api.mergeTasks(sourceIds, destId);
                showToast(lang === 'ko' ? '병합 완료' : 'Merged successfully', 'success');
                clearTaskSelection();
                updateMergeBar();
                fetchMessages(true);
            } catch (e: any) {
                showToast(e.message || 'Merge failed', 'error');
            }
        };
        confirmBtn?.addEventListener('click', handleConfirm, { once: true });
    });

    document.getElementById('clearSelectionBtn')?.addEventListener('click', () => {
        clearTaskSelection();
        updateMergeBar();
        // Use local render for speed instead of full fetch
        renderMessages(state.messages);
    });
};

/**
 * Recursive Polling Helper to prevent overlapping requests.
 */
const schedulePoll = (task: (bypass: boolean) => Promise<void>, interval: number) => {
    const timer = setTimeout(async () => {
        try {
            await task(false);
        } finally {
            const idx = activeTimers.indexOf(timer);
            if (idx !== -1) activeTimers.splice(idx, 1);
            schedulePoll(task, interval);
        }
    }, interval);
    activeTimers.push(timer);
};

/**
 * Initializes tab visibility listener for immediate refresh.
 */
const initVisibilityListener = () => {
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) {
            console.log('[I18n] Tab active! Refreshing data and status');
            // Explicitly sync when coming back to the tab
            fetchMessages(true);
            checkAllStatus(true);
        }
    });
};

/**
 * Initializes background polling.
 */
const initPolling = () => {
    activeTimers.forEach(clearTimeout);
    activeTimers.length = 0;

    // Consolidated polling for messages and statuses
    schedulePoll(fetchMessages, POLLING_INTERVALS.MESSAGES);
    schedulePoll(checkAllStatus, POLLING_INTERVALS.WHATSAPP); 
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

    renderUILanguage(state.currentLang);
    initTheme();
    initLanguageSelector();

    setupTabs('#dashboardContent .tab-btn', '#dashboardContent .c-tabs__panel', 'data-tab', 'active', async () => {
        await fetchMessages(true);
    });
    setupTabs('.c-settings__tab', '.c-settings__panel', 'data-settings-tab', 'c-settings__tab--active', (tabId: string) => {
        if (tabId === 'tokenUsageTab') {
            modals.fetchTokenUsage();
        }
    });
    setTimeout(() => (document.querySelector('[data-tab="receivedTasksTab"]') as HTMLElement)?.click(), 500);

    initNavigation();
    initActionButtons();
    
    // Initialize Event Delegation for all grids
    ['receivedTasksList', 'delegatedTasksList', 'referenceTasksList', 'allTasksList'].forEach(id => {
        initMessageGridEvents(id, handlers);
    });

    archive.init(fetchMessages);
    modals.init(fetchMessages);
    insights.init?.();

    fetchUserProfile();
    checkAllStatus(true);

    initPolling();
    initVisibilityListener();
};

document.addEventListener('DOMContentLoaded', initApp);
