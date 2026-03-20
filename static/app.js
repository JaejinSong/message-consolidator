import { state, updateLang, updateTheme } from './js/state.js';
import { updateUILanguage } from './js/i18n.js';
import { I18N_DATA } from './js/locales.js';
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
        const data = await api.fetchMessages(state.currentLang);
        renderer.renderMessages(data, handlers);
        renderer.updateSlackStatus(data);
    } catch (e) { console.error(e); }
};

const fetchArchive = async () => {
    const loader = document.getElementById('archiveLoading');
    if (loader) loader.classList.add('active');
    try {
        const params = {
            q: state.archiveSearch,
            limit: state.archiveLimit,
            offset: (state.archivePage - 1) * state.archiveLimit,
            lang: state.currentLang,
            sort: state.archiveSort,
            order: state.archiveOrder
        };
        const data = await api.fetchArchive(params);
        state.archiveTotalCount = data.total;
        renderer.renderArchive(data.messages);
        updateArchivePaginationUI();
    } catch (e) {
        console.error(e);
    } finally {
        if (loader) loader.classList.remove('active');
    }
};

const updateArchivePaginationUI = () => {
    const totalPages = Math.ceil(state.archiveTotalCount / state.archiveLimit) || 1;
    const pageInfo = document.getElementById('archivePageInfo');
    const i18n = I18N_DATA[state.currentLang];

    if (pageInfo && i18n) {
        let text = i18n.archivePageInfo || `Page {page} / {totalPages} (Total: {totalCount})`;
        text = text.replace('{page}', state.archivePage)
            .replace('{totalPages}', totalPages)
            .replace('{totalCount}', state.archiveTotalCount);
        pageInfo.textContent = text;
    }

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
    const rawValue = input.value;
    if (!rawValue.trim()) return;

    const aliases = rawValue.split(',').map(a => a.trim()).filter(a => a);
    try {
        await Promise.all(aliases.map(a => api.addAlias(a)));
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

const fetchTenantAliases = async () => {
    try {
        const aliases = await api.fetchTenantAliases();
        renderer.renderTenantAliasList(aliases, removeTenantAliasMapping);
    } catch (e) { console.error(e); }
};

const addTenantAliasMapping = async () => {
    const origInput = document.getElementById('normOriginalInput');
    const primInput = document.getElementById('normPrimaryInput');
    const original = origInput.value.trim();
    const primary = primInput.value.trim();
    if (!original || !primary) return;
    try {
        await api.addTenantAlias(original, primary);
        origInput.value = '';
        primInput.value = '';
        fetchTenantAliases();
    } catch (e) { console.error(e); }
};

const removeTenantAliasMapping = async (original) => {
    try {
        await api.removeTenantAlias(original);
        fetchTenantAliases();
    } catch (e) { console.error(e); }
};

const fetchTokenUsage = async () => {
    try {
        const usage = await api.fetchTokenUsage();
        renderer.updateTokenBadge(usage);
    } catch (e) { console.error(e); }
};

const fetchContactMappings = async () => {
    try {
        const mappings = await api.fetchContactMappings();
        renderer.renderContactMappings(mappings, removeContactMapping);
    } catch (e) { console.error(e); }
};

const addContactMapping = async () => {
    const repInput = document.getElementById('contactRepInput');
    const aliasInput = document.getElementById('contactAliasesInput');
    const repName = repInput.value.trim();
    const aliases = aliasInput.value.trim();
    if (!repName || !aliases) return;
    try {
        await api.addContactMapping(repName, aliases);
        repInput.value = '';
        aliasInput.value = '';
        fetchContactMappings();
    } catch (e) { console.error(e); }
};

const removeContactMapping = async (repName) => {
    try {
        await api.removeContactMapping(repName);
        fetchContactMappings();
    } catch (e) { console.error(e); }
};
window.removeContactMapping = removeContactMapping;

// Global helper for Quick Alias Mapping from UI
window.openAliasMapping = (name) => {
    const settingsModal = document.getElementById('settingsModal');
    if (settingsModal) {
        settingsModal.classList.remove('hidden');

        // Settings 모달을 열 때 Mappings 탭으로 전환
        const mappingsTabBtn = document.querySelector('[data-settings-tab="mappingsTab"]');
        if (mappingsTabBtn) mappingsTabBtn.click();

        fetchTenantAliases();
        fetchContactMappings();

        const origInput = document.getElementById('normOriginalInput');
        const contactAliasInput = document.getElementById('contactAliasesInput');
        if (origInput) {
            origInput.value = name;
            if (contactAliasInput) contactAliasInput.value = name;
            document.getElementById('normPrimaryInput')?.focus();
        }
    }
};


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
                if (!document.getElementById('archiveSection').classList.contains('hidden')) {
                    fetchArchive();
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

    // Archive
    const updateArchiveActionsVisibility = () => {
        const checkedCount = document.querySelectorAll('.archive-check:checked').length;
        const restoreBtn = document.getElementById('restoreSelectedBtn');
        const hardDeleteBtn = document.getElementById('hardDeleteSelectedBtn');
        if (restoreBtn) restoreBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
        if (hardDeleteBtn) hardDeleteBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
    };

    const getSelectedArchiveIds = () => {
        return Array.from(document.querySelectorAll('.archive-check:checked')).map(cb => parseInt(cb.getAttribute('data-id')));
    };

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
            // Reset selection
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            updateArchiveActionsVisibility();
            fetchArchive();
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

    document.getElementById('selectAllArchive')?.addEventListener('change', (e) => {
        const checked = e.target.checked;
        document.querySelectorAll('.archive-check').forEach(cb => cb.checked = checked);
        updateArchiveActionsVisibility();
    });

    document.getElementById('archiveBody')?.addEventListener('change', (e) => {
        if (e.target.classList.contains('archive-check')) {
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
        } catch (e) {
            alert((I18N_DATA[state.currentLang]?.errorArchiveCount || 'Error: ') + e.message);
        }
    });

    const closeExport = () => exportModal.classList.add('hidden');
    document.getElementById('closeExportModalBtn')?.addEventListener('click', closeExport);
    document.getElementById('cancelExportBtn')?.addEventListener('click', closeExport);

    const downloadFile = (url, defaultFilename) => {
        console.log(`[DEBUG] Starting native download: ${url}, default: ${defaultFilename}`);
        const loading = document.getElementById('loading');
        if (loading) loading.classList.remove('hidden');

        // 브라우저 네이티브 다운로드 방식을 사용하여 Chrome의 UUID 버그를 원천 차단합니다.
        const a = document.createElement('a');
        a.style.display = 'none';
        a.href = url;
        a.download = defaultFilename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);

        // 네이티브 다운로드는 콜백이 없으므로 일정 시간 후 로딩 스피너를 숨깁니다.
        setTimeout(() => {
            if (loading) loading.classList.add('hidden');
        }, 2000);
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

    document.getElementById('confirmExportJson')?.addEventListener('click', () => {
        const query = state.archiveSearch ? `?q=${encodeURIComponent(state.archiveSearch)}` : '';
        const timestamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '_');
        downloadFile(`/api/messages/export/json${query}`, `Message_Archive_${timestamp}.json`);
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
        } catch (e) {
            alert((I18N_DATA[state.currentLang]?.errorRestore || 'Error: ') + e.message);
        }
    });

    // Custom Delete Confirmation Modal Logic
    const deleteConfirmModal = document.getElementById('deleteConfirmModal');
    let deletePendingIds = [];

    document.getElementById('hardDeleteSelectedBtn')?.addEventListener('click', () => {
        deletePendingIds = getSelectedArchiveIds();
        if (deletePendingIds.length === 0) return;

        const countSpan = document.getElementById('deleteConfirmCount');
        if (countSpan) countSpan.textContent = deletePendingIds.length;
        deleteConfirmModal.classList.remove('hidden');
    });

    const closeDeleteConfirm = () => {
        deleteConfirmModal.classList.add('hidden');
        deletePendingIds = [];
    };

    document.getElementById('closeDeleteConfirmBtn')?.addEventListener('click', closeDeleteConfirm);
    document.getElementById('cancelDeleteConfirmBtn')?.addEventListener('click', closeDeleteConfirm);

    window.addEventListener('click', (e) => {
        if (e.target === deleteConfirmModal) closeDeleteConfirm();
    });

    document.getElementById('confirmHardDeleteBtn')?.addEventListener('click', async () => {
        if (deletePendingIds.length === 0) return;
        try {
            await api.hardDeleteTasks(deletePendingIds);
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            updateArchiveActionsVisibility();
            fetchArchive();
            closeDeleteConfirm();
        } catch (error) {
            alert((I18N_DATA[state.currentLang]?.errorHardDelete || 'Error: ') + error.message);
        }
    });


    // Archive sorting listeners
    const triggerArchiveSort = (field) => {
        if (state.archiveSort === field) {
            state.archiveOrder = state.archiveOrder === 'ASC' ? 'DESC' : 'ASC';
        } else {
            state.archiveSort = field;
            state.archiveOrder = 'DESC';
        }
        state.archivePage = 1;
        fetchArchive();
    };

    document.getElementById('ahSource')?.addEventListener('click', () => triggerArchiveSort('source'));
    document.getElementById('ahRoom')?.addEventListener('click', () => triggerArchiveSort('room'));
    document.getElementById('ahTask')?.addEventListener('click', () => triggerArchiveSort('task'));
    document.getElementById('ahRequester')?.addEventListener('click', () => triggerArchiveSort('requester'));
    document.getElementById('ahAssignee')?.addEventListener('click', () => triggerArchiveSort('assignee'));
    document.getElementById('ahTime')?.addEventListener('click', () => triggerArchiveSort('time'));
    document.getElementById('ahCompletedAt')?.addEventListener('click', () => triggerArchiveSort('completed_at'));

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

    // Release Notes
    const releaseNotesModal = document.getElementById('releaseNotesModal');
    const showReleaseNotes = async () => {
        try {
            const data = await api.fetchReleaseNotes();
            if (data && data.content) {
                renderer.renderReleaseNotes(data.content);
            }
        } catch (e) {
            console.error('Failed to fetch release notes:', e);
        }
    };

    document.getElementById('releaseNotesBtn')?.addEventListener('click', showReleaseNotes);
    document.getElementById('closeReleaseNotesBtn')?.addEventListener('click', () => {
        releaseNotesModal.classList.add('hidden');
    });
    document.getElementById('confirmReleaseNotesBtn')?.addEventListener('click', () => {
        releaseNotesModal.classList.add('hidden');
    });
    window.addEventListener('click', (e) => {
        if (e.target === releaseNotesModal) releaseNotesModal.classList.add('hidden');
    });

    // Original Message Modal
    const originalModal = document.getElementById('originalMessageModal');
    document.getElementById('closeOriginalBtn')?.addEventListener('click', () => {
        originalModal.classList.add('hidden');
    });
    window.addEventListener('click', (e) => {
        if (e.target === originalModal) originalModal.classList.add('hidden');
    });

    // Settings
    const settingsModal = document.getElementById('settingsModal');
    document.getElementById('settingsBtn')?.addEventListener('click', () => {
        settingsModal.classList.remove('hidden');
        renderer.renderAliasList(state.userAliases, removeAlias);
        fetchTenantAliases();
        fetchContactMappings();
        fetchTokenUsage();
    });

    document.getElementById('closeSettingsBtn')?.addEventListener('click', () => {
        settingsModal.classList.add('hidden');
    });

    window.addEventListener('click', (e) => {
        if (e.target === settingsModal) settingsModal.classList.add('hidden');
    });

    document.getElementById('updatesBtn')?.addEventListener('click', showReleaseNotes);

    document.getElementById('addAliasBtn')?.addEventListener('click', addAlias);
    document.getElementById('newAliasInput')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') addAlias();
    });

    document.getElementById('addNormBtn')?.addEventListener('click', addTenantAliasMapping);
    document.getElementById('normPrimaryInput')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') addTenantAliasMapping();
    });

    document.getElementById('addContactBtn')?.addEventListener('click', addContactMapping);
    document.getElementById('contactAliasesInput')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') addContactMapping();
    });

    updateUILanguage(state.currentLang); // 초기 언어 적용
    fetchUserProfile();                  // 내부에서 fetchMessages()를 이어 호출함
    checkWhatsAppStatus();
    checkGmailStatus();

    // 주기적 업데이트
    setInterval(fetchMessages, 29009);
    setInterval(checkWhatsAppStatus, 31013);
    setInterval(checkGmailStatus, 61001);

    // Gmail icon click: connect when OFF, show info when ON
    document.getElementById('gmailStatusLarge')?.addEventListener('click', () => {
        if (!state.gmailConnected) {
            window.location.href = '/auth/gmail/connect';
        }
    });
};

document.addEventListener('DOMContentLoaded', initApp);
