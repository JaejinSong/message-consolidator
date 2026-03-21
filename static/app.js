import { state, updateLang, updateTheme } from './js/state.js';
import { updateUILanguage } from './js/i18n.js';
import { I18N_DATA } from './js/locales.js';
import { api } from './js/api.js';
import { renderer } from './js/renderer.js';
import { archive } from './js/archive.js';
import { modals } from './js/modals.js';
import { events, EVENTS } from './js/events.js';

// --- Global Event Handlers for Renderer ---
const handlers = {
    async onToggleDone(id, done) {
        try {
            await api.toggleDone(id, done);
            if (done) {
                events.emit(EVENTS.TASK_COMPLETED, { id });
            } else {
                fetchMessages();
            }
        } catch (e) { console.error(e); }
    },
    async onDeleteTask(id) {
        try {
            await api.deleteTask(id);
            fetchMessages();
            if (archive.isVisible()) {
                archive.fetch();
            }
        } catch (e) { console.error(e); }
    }
};

// --- Core Logic ---
const fetchMessages = async () => {
    try {
        const data = await api.fetchMessages(state.currentLang);
        renderer.renderMessages(data, handlers);
    } catch (e) { console.error(e); }
};

const checkSlackStatus = async () => {
    try {
        const data = await api.fetchSlackStatus();
        renderer.updateSlackStatus(data.status === 'CONNECTED');
    } catch (e) { console.error(e); }
};

const checkWhatsAppStatus = async () => {
    try {
        const data = await api.fetchWhatsAppStatus();
        renderer.updateWhatsAppStatus(data.status);
    } catch (e) { console.error(e); }
};

const checkGmailStatus = async () => {
    try {
        const data = await api.fetchGmailStatus();
        renderer.updateGmailStatus(data.connected);
    } catch (e) { console.error(e); }
};

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
            // Use status-label text from i18n but keep it concise for the small box
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

const fetchUserProfile = async () => {
    try {
        const data = await api.fetchUserProfile();
        state.userProfile = data;
        state.userAliases = data.aliases || [];
        events.emit(EVENTS.USER_PROFILE_UPDATED, state.userProfile);
        fetchMessages();
    } catch (e) {
        console.error(e);
        fetchMessages();
    }
};

// --- Event Subscriptions ---
events.on(EVENTS.TASK_COMPLETED, (data) => {
    renderer.triggerConfetti();
    renderer.triggerXPAnimation();
    fetchUserProfile(); // This will update stats via EVENT.USER_PROFILE_UPDATED
});

events.on(EVENTS.USER_PROFILE_UPDATED, (profile) => {
    renderer.updateUserProfile(profile);
});

// --- Initialization ---
const initApp = () => {
    console.log("Initializing Modular App...");

    updateUILanguage(state.currentLang);

    // Theme initialization
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

    const switchTab = (tabId) => {
        const tabs = document.querySelectorAll('.tab-btn:not(.settings-tab-btn)');
        const contents = document.querySelectorAll('.tab-content:not(.settings-tab-content)');
        tabs.forEach(b => b.classList.remove('active'));
        contents.forEach(c => c.classList.remove('active'));
        const activeBtn = document.querySelector(`[data-tab="${tabId}"]`);
        const activeContent = document.getElementById(tabId);
        if (activeBtn) activeBtn.classList.add('active');
        if (activeContent) activeContent.classList.add('active');
    };

    document.querySelectorAll('.tab-btn:not(.settings-tab-btn)').forEach(btn => {
        btn.addEventListener('click', () => switchTab(btn.getAttribute('data-tab')));
    });

    // Settings Tab Switching
    const switchSettingsTab = (tabId) => {
        const tabs = document.querySelectorAll('.settings-tab-btn');
        const contents = document.querySelectorAll('.settings-tab-content');
        tabs.forEach(b => b.classList.remove('active'));
        contents.forEach(c => c.classList.remove('active'));
        const activeBtn = document.querySelector(`[data-settings-tab="${tabId}"]`);
        const activeContent = document.getElementById(tabId);
        if (activeBtn) activeBtn.classList.add('active');
        if (activeContent) activeContent.classList.add('active');
    };

    document.querySelectorAll('.settings-tab-btn').forEach(btn => {
        btn.addEventListener('click', () => switchSettingsTab(btn.getAttribute('data-settings-tab')));
    });

    setTimeout(() => switchTab('myTasksTab'), 500);

    // Initialize Archive Controller
    archive.init(fetchMessages);

    // Initialize Modals Controller
    modals.init(fetchMessages);

    // --- Unified View Switching (Dashboard vs Archive) ---
    const showView = (view) => {
        const dashboardTabs = document.querySelector('.tabs-container');
        const dashboardHeader = document.querySelector('.dashboard-header');
        const archiveSection = document.getElementById('archiveSection');
        const navTabs = document.querySelectorAll('.nav-tab');

        if (view === 'archive') {
            dashboardTabs?.classList.add('hidden');
            dashboardHeader?.classList.add('hidden');
            archiveSection?.classList.remove('hidden');
            archive.onShow();
        } else {
            dashboardTabs?.classList.remove('hidden');
            dashboardHeader?.classList.remove('hidden');
            archiveSection?.classList.add('hidden');
            fetchMessages();
        }

        // Active state update
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

    // WhatsApp QR
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

    updateUILanguage(state.currentLang); // 초기 언어 적용
    fetchUserProfile();                  // 내부에서 fetchMessages()를 이어 호출함
    checkWhatsAppStatus();
    checkSlackStatus();
    checkGmailStatus();
    modals.fetchTokenUsage(); // 대시보드 로딩 시 우측 상단 토큰 배지 업데이트

    // 주기적 업데이트
    setInterval(fetchMessages, 29009);
    setInterval(checkWhatsAppStatus, 31013);
    setInterval(checkSlackStatus, 41017);
    setInterval(checkGmailStatus, 61001);
    setInterval(() => modals.fetchTokenUsage(), 60000); // 1분마다 토큰 사용량 동기화

    // Gmail icon click: connect when OFF, show info when ON
    document.getElementById('gmailStatusLarge')?.addEventListener('click', () => {
        if (!state.gmailConnected) {
            window.location.href = '/auth/gmail/connect';
        }
    });
};

document.addEventListener('DOMContentLoaded', initApp);
