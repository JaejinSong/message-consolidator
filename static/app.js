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
        const params = {
            q: state.archiveSearch,
            limit: state.archiveLimit,
            offset: (state.archivePage - 1) * state.archiveLimit
        };
        const data = await api.fetchArchive(params);
        state.archiveTotalCount = data.total;
        renderer.renderArchive(data.messages);
        updateArchivePaginationUI();
    } catch (e) { console.error(e); }
};

const updateArchivePaginationUI = () => {
    const totalPages = Math.ceil(state.archiveTotalCount / state.archiveLimit) || 1;
    const pageInfo = document.getElementById('archivePageInfo');
    if (pageInfo) pageInfo.textContent = `Page ${state.archivePage} / ${totalPages} (Total: ${state.archiveTotalCount})`;
    
    const prevBtn = document.getElementById('prevArchivePage');
    const nextBtn = document.getElementById('nextArchivePage');
    if (prevBtn) prevBtn.disabled = state.archivePage <= 1;
    if (nextBtn) nextBtn.disabled = state.archivePage >= totalPages;
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
    const updateArchiveActionsVisibility = () => {
        const checkedCount = document.querySelectorAll('.archive-checkbox:checked').length;
        const restoreBtn = document.getElementById('restoreSelectedBtn');
        const hardDeleteBtn = document.getElementById('hardDeleteSelectedBtn');
        if (restoreBtn) restoreBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
        if (hardDeleteBtn) hardDeleteBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
    };

    const getSelectedArchiveIds = () => {
        return Array.from(document.querySelectorAll('.archive-checkbox:checked')).map(cb => parseInt(cb.getAttribute('data-id')));
    };

    document.getElementById('archiveLink')?.addEventListener('click', (e) => {
        e.preventDefault();
        document.querySelector('.tabs-container')?.classList.add('hidden');
        document.querySelector('.dashboard-header')?.classList.add('hidden');
        document.getElementById('archiveSection')?.classList.remove('hidden');
        // Reset selection
        const selectAll = document.getElementById('selectAllArchive');
        if (selectAll) selectAll.checked = false;
        updateArchiveActionsVisibility();
        fetchArchive();
    });

    const closeArchive = () => {
        document.querySelector('.tabs-container')?.classList.remove('hidden');
        document.querySelector('.dashboard-header')?.classList.remove('hidden');
        document.getElementById('archiveSection')?.classList.add('hidden');
    };

    document.getElementById('closeArchiveBtn')?.addEventListener('click', closeArchive);
    document.getElementById('backToDashBtn')?.addEventListener('click', closeArchive);

    document.getElementById('selectAllArchive')?.addEventListener('change', (e) => {
        const checked = e.target.checked;
        document.querySelectorAll('.archive-checkbox').forEach(cb => cb.checked = checked);
        updateArchiveActionsVisibility();
    });

    document.getElementById('archiveBody')?.addEventListener('change', (e) => {
        if (e.target.classList.contains('archive-checkbox')) {
            updateArchiveActionsVisibility();
        }
    });

    // Archive Search & Pagination
    let searchTimeout;
    document.getElementById('archiveSearchInput')?.addEventListener('input', (e) => {
        clearTimeout(searchTimeout);
        searchTimeout = setTimeout(() => {
            state.archiveSearch = e.target.value;
            state.archivePage = 1; // Reset to page 1 on search
            fetchArchive();
        }, 500);
    });

    document.getElementById('prevArchivePage')?.addEventListener('click', () => {
        if (state.archivePage > 1) {
            state.archivePage--;
            fetchArchive();
        }
    });

    document.getElementById('nextArchivePage')?.addEventListener('click', () => {
        const totalPages = Math.ceil(state.archiveTotalCount / state.archiveLimit);
        if (state.archivePage < totalPages) {
            state.archivePage++;
            fetchArchive();
        }
    });

    // Export Modal Logic
    const exportModal = document.getElementById('exportModal');
    document.getElementById('openExportModalBtn')?.addEventListener('click', async () => {
        try {
            const countData = await api.fetchArchiveCount(state.archiveSearch);
            document.getElementById('exportCount').textContent = countData.count;
            exportModal.classList.remove('hidden');
        } catch (e) { alert('Failed to get archive count: ' + e.message); }
    });

    const closeExport = () => exportModal.classList.add('hidden');
    document.getElementById('closeExportModalBtn')?.addEventListener('click', closeExport);
    document.getElementById('cancelExportBtn')?.addEventListener('click', closeExport);
    
    const downloadFile = async (url, defaultFilename) => {
        console.log(`[DEBUG] Starting download: ${url}, default: ${defaultFilename}`);
        const loading = document.getElementById('loading');
        loading.classList.remove('hidden');
        try {
            const resp = await fetch(url, { credentials: 'same-origin' });
            if (!resp.ok) throw new Error(`Download failed: ${resp.status}`);
            
            // Try to get filename from header
            const disposition = resp.headers.get('Content-Disposition');
            console.log(`[DEBUG] Disposition header: ${disposition}`);
            let filename = defaultFilename;
            if (disposition && disposition.indexOf('filename=') !== -1) {
                const filenameRegex = /filename[^;=\n]*=((['"]).*?\2|[^;\n]*)/;
                const matches = filenameRegex.exec(disposition);
                if (matches != null && matches[1]) {
                    filename = matches[1].replace(/['"]/g, '');
                }
            }
            console.log(`[DEBUG] Final filename: ${filename}`);
            
            const rawBlob = await resp.blob();
            const blob = new Blob([rawBlob], { type: 'application/octet-stream' });
            const blobUrl = window.URL.createObjectURL(blob);
            
            const a = document.createElement('a');
            a.style.display = 'none';
            a.href = blobUrl;
            a.download = filename;
            document.body.appendChild(a);
            console.log(`[DEBUG] Triggering browser download for: ${filename}`);
            a.click();
            document.body.removeChild(a);
            
            // Wait a bit before cleanup to ensure browser starts download
            setTimeout(() => {
                window.URL.revokeObjectURL(blobUrl);
                document.body.removeChild(a);
                console.log(`[DEBUG] Download triggered for ${filename}`);
            }, 1000);
        } catch (e) {
            console.error('[DEBUG] Download error:', e);
            alert('Download failed: ' + e.message);
        } finally {
            loading.classList.add('hidden');
        }
    };

    document.getElementById('confirmExportExcel')?.addEventListener('click', () => {
        const query = state.archiveSearch ? `?q=${encodeURIComponent(state.archiveSearch)}` : '';
        const timestamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '_');
        downloadFile(`/api/messages/export/excel${query}`, `Message_Archive_${timestamp}.xlsx`);
        closeExport();
    });

    document.getElementById('confirmExportCsv')?.addEventListener('click', () => {
        const query = state.archiveSearch ? `?q=${encodeURIComponent(state.archiveSearch)}` : '';
        const timestamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '_');
        downloadFile(`/api/messages/export${query}`, `Message_Archive_${timestamp}.csv`);
        closeExport();
    });

    document.getElementById('restoreSelectedBtn')?.addEventListener('click', async () => {
        const ids = getSelectedArchiveIds();
        if (ids.length === 0) return;
        try {
            await api.restoreTasks(ids);
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            updateArchiveActionsVisibility();
            fetchArchive();
            fetchMessages();
        } catch (e) { alert('Restore failed: ' + e.message); }
    });

    document.getElementById('hardDeleteSelectedBtn')?.addEventListener('click', async () => {
        const ids = getSelectedArchiveIds();
        if (ids.length === 0) return;
        if (!confirm(`Are you sure you want to PERMANENTLY delete ${ids.length} items?`)) return;
        try {
            await api.hardDeleteTasks(ids);
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            updateArchiveActionsVisibility();
            fetchArchive();
        } catch (e) { alert('Hard delete failed: ' + e.message); }
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
