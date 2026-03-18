import { state, updateLang } from './js/state.js';
import { I18N_DATA, updateUILanguage } from './js/i18n.js';
import { api } from './js/api.js';
import { renderer } from './js/renderer.js';

// --- Global Event Handlers for Renderer ---
const handlers = {
    async onToggleDone(id, done) {
        try {
            await api.toggleDone(id, done);
            fetchMessages();
        } catch (e) { console.error(e); }
    },
    async onDeleteTask(id) {
        if (!confirm("Are you sure you want to delete this task? It will be moved to the archive.")) return;
        try {
            await api.deleteTask(id);
            fetchMessages();
            if (!document.getElementById('archiveSection').classList.contains('hidden')) {
                fetchArchive();
            }
        } catch (e) { console.error(e); }
    }
};

// --- Core Logic ---
const fetchMessages = async () => {
    try {
        const data = await api.fetchMessages();
        renderer.renderMessages(data, handlers);
        renderer.updateSlackStatus(data);
    } catch (e) { console.error(e); }
};

const fetchArchive = async () => {
    try {
        const data = await api.fetchArchive();
        renderer.renderArchive(data);
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
            if (scanBtnText) scanBtnText.textContent = 'SCAN';
            if (loading) loading.classList.add('hidden');
        }, 5000);
    } catch (e) {
        console.error(e);
        if (btn) btn.disabled = false;
        if (scanBtnText) scanBtnText.textContent = 'SCAN';
        if (loading) loading.classList.add('hidden');
    }
};

const fetchUserProfile = async () => {
    try {
        const data = await api.fetchUserProfile();
        state.userProfile = data;
        state.userAliases = data.aliases || [];
        renderer.updateUserProfile(state.userProfile);
        fetchMessages();
    } catch (e) {
        console.error(e);
        fetchMessages();
    }
};

const fetchAliases = async () => {
    try {
        const aliases = await api.fetchAliases();
        state.userAliases = aliases;
        renderer.renderAliasList(state.userAliases, removeAlias);
        fetchMessages();
    } catch (e) { console.error(e); }
};

const addAlias = async () => {
    const input = document.getElementById('newAliasInput');
    const alias = input.value.trim();
    if (!alias) return;
    try {
        await api.addAlias(alias);
        input.value = '';
        fetchAliases();
    } catch (e) { console.error(e); }
};

const removeAlias = async (alias) => {
    try {
        await api.removeAlias(alias);
        fetchAliases();
    } catch (e) { console.error(e); }
};

// --- Initialization ---
const initApp = () => {
    console.log("Initializing Modular App...");
    
    updateUILanguage(state.currentLang);

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
            } finally {
                loading.classList.add('hidden');
            }
        });
    }

    // Tab Switching
    const switchTab = (tabId) => {
        const tabs = document.querySelectorAll('.tab-btn');
        const contents = document.querySelectorAll('.tab-content');
        tabs.forEach(b => b.classList.remove('active'));
        contents.forEach(c => c.classList.remove('active'));
        const activeBtn = document.querySelector(`[data-tab="${tabId}"]`);
        const activeContent = document.getElementById(tabId);
        if (activeBtn) activeBtn.classList.add('active');
        if (activeContent) activeContent.classList.add('active');
    };

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => switchTab(btn.getAttribute('data-tab')));
    });

    setTimeout(() => switchTab('myTasksTab'), 500);

    // Archive
    document.getElementById('archiveLink')?.addEventListener('click', (e) => {
        e.preventDefault();
        document.querySelector('.tabs-container')?.classList.add('hidden');
        document.querySelector('.dashboard-header')?.classList.add('hidden');
        document.getElementById('archiveSection')?.classList.remove('hidden');
        fetchArchive();
    });

    document.getElementById('closeArchiveBtn')?.addEventListener('click', () => {
        document.querySelector('.tabs-container')?.classList.remove('hidden');
        document.querySelector('.dashboard-header')?.classList.remove('hidden');
        document.getElementById('archiveSection')?.classList.add('hidden');
    });

    document.getElementById('exportCsvBtn')?.addEventListener('click', () => {
        window.location.href = '/api/messages/archive/export';
    });

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
                }, 3000);
            }
        } catch (e) {
            placeholder.textContent = 'Error';
            alert(i18n.qrError + e.message);
            btn.disabled = false;
        }
    });

    document.getElementById('scanBtn')?.addEventListener('click', triggerScan);

    // Settings
    const settingsModal = document.getElementById('settingsModal');
    document.getElementById('settingsBtn')?.addEventListener('click', () => {
        settingsModal.classList.remove('hidden');
        renderer.renderAliasList(state.userAliases, removeAlias);
    });

    document.getElementById('closeSettingsBtn')?.addEventListener('click', () => {
        settingsModal.classList.add('hidden');
    });

    window.addEventListener('click', (e) => {
        if (e.target === settingsModal) settingsModal.classList.add('hidden');
    });

    document.getElementById('addAliasBtn')?.addEventListener('click', addAlias);
    document.getElementById('newAliasInput')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') addAlias();
    });

    fetchUserProfile();
    checkWhatsAppStatus();
    checkGmailStatus();
    setInterval(fetchMessages, 30000);
    setInterval(checkWhatsAppStatus, 30000);
    setInterval(checkGmailStatus, 60000);

    // Gmail icon click: connect when OFF, show info when ON
    document.getElementById('gmailStatusLarge')?.addEventListener('click', () => {
        if (!state.gmailConnected) {
            window.location.href = '/auth/gmail/connect';
        }
    });
};

document.addEventListener('DOMContentLoaded', initApp);
