import { state } from './state.js';
import { I18N_DATA } from './locales.js';
import { api } from './api.js';
import { renderer } from './renderer.js';
import { safeAsync } from './utils.js';

let onTasksChangedCallback = null;

export const archive = {
    init(fetchMessagesCallback) {
        onTasksChangedCallback = fetchMessagesCallback;
        this.setupEventListeners();
    },

    isVisible() {
        return !document.getElementById('archiveSection').classList.contains('hidden');
    },

    onShow() {
        const selectAll = document.getElementById('selectAllArchive');
        if (selectAll) selectAll.checked = false;
        this.updateActionsVisibility();
        this.fetch();
    },

    fetch: safeAsync(async function () {
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
            this.updatePaginationUI();
        } finally {
            if (loader) loader.classList.remove('active');
        }
    }),

    updatePaginationUI() {
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
    },

    updateActionsVisibility() {
        const checkedCount = document.querySelectorAll('.archive-check:checked').length;
        const restoreBtn = document.getElementById('restoreSelectedBtn');
        const hardDeleteBtn = document.getElementById('hardDeleteSelectedBtn');
        if (restoreBtn) restoreBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
        if (hardDeleteBtn) hardDeleteBtn.style.display = checkedCount > 0 ? 'inline-block' : 'none';
    },

    getSelectedIds() {
        return Array.from(document.querySelectorAll('.archive-check:checked')).map(cb => parseInt(cb.getAttribute('data-id')));
    },

    setupEventListeners() {
        document.getElementById('selectAllArchive')?.addEventListener('change', (e) => {
            const checked = e.target.checked;
            document.querySelectorAll('.archive-check').forEach(cb => cb.checked = checked);
            this.updateActionsVisibility();
        });

        document.getElementById('archiveBody')?.addEventListener('change', (e) => {
            if (e.target.classList.contains('archive-check')) {
                this.updateActionsVisibility();
            }
        });

        let searchTimeout;
        document.getElementById('archiveSearchInput')?.addEventListener('input', (e) => {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                state.archiveSearch = e.target.value;
                state.archivePage = 1;
                this.fetch();
            }, 500);
        });

        document.getElementById('prevArchivePage')?.addEventListener('click', () => {
            if (state.archivePage > 1) {
                state.archivePage--;
                this.fetch();
            }
        });

        document.getElementById('nextArchivePage')?.addEventListener('click', () => {
            const totalPages = Math.ceil(state.archiveTotalCount / state.archiveLimit);
            if (state.archivePage < totalPages) {
                state.archivePage++;
                this.fetch();
            }
        });

        // Restore Selected
        document.getElementById('restoreSelectedBtn')?.addEventListener('click', safeAsync(async () => {
            const ids = this.getSelectedIds();
            if (ids.length === 0) return;

            await api.restoreTasks(ids);
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            this.updateActionsVisibility();
            this.fetch();
            if (onTasksChangedCallback) onTasksChangedCallback();
        }, (e) => {
            renderer.showToast((I18N_DATA[state.currentLang]?.errorRestore || 'Error: ') + e.message, 'error');
        }));

        // Sorting
        const triggerArchiveSort = (field) => {
            if (state.archiveSort === field) {
                state.archiveOrder = state.archiveOrder === 'ASC' ? 'DESC' : 'ASC';
            } else {
                state.archiveSort = field;
                state.archiveOrder = 'DESC';
            }
            state.archivePage = 1;
            this.fetch();
        };

        const sortHeaders = {
            'ahSource': 'source', 'ahRoom': 'room', 'ahTask': 'task',
            'ahRequester': 'requester', 'ahAssignee': 'assignee',
            'ahTime': 'time', 'ahCompletedAt': 'completed_at'
        };
        Object.entries(sortHeaders).forEach(([id, field]) => {
            document.getElementById(id)?.addEventListener('click', () => triggerArchiveSort(field));
        });

        this.setupExportModal();
        this.setupDeleteModal();
    },

    setupExportModal() {
        const exportModal = document.getElementById('exportModal');
        document.getElementById('openExportModalBtn')?.addEventListener('click', safeAsync(async () => {
            const countData = await api.fetchArchiveCount(state.archiveSearch);
            document.getElementById('exportCount').textContent = countData.count;
            exportModal.classList.remove('hidden');
        }, (e) => {
            renderer.showToast((I18N_DATA[state.currentLang]?.errorArchiveCount || 'Error: ') + e.message, 'error');
        }));

        const downloadFile = (url, defaultFilename) => {
            console.log(`[DEBUG] Starting native download: ${url}, default: ${defaultFilename}`);
            const loading = document.getElementById('loading');
            if (loading) loading.classList.remove('hidden');

            const a = document.createElement('a');
            a.style.display = 'none';
            a.href = url;
            a.download = defaultFilename;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);

            setTimeout(() => {
                if (loading) loading.classList.add('hidden');
            }, 2000);
        };

        const exportFormats = [
            { id: 'confirmExportExcel', path: '/api/messages/export/excel', ext: 'xlsx' },
            { id: 'confirmExportCsv', path: '/api/messages/export', ext: 'csv' },
            { id: 'confirmExportJson', path: '/api/messages/export/json', ext: 'json' }
        ];

        exportFormats.forEach(({ id, path, ext }) => {
            document.getElementById(id)?.addEventListener('click', () => {
                const query = state.archiveSearch ? `?q=${encodeURIComponent(state.archiveSearch)}` : '';
                const timestamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '_');
                downloadFile(`${path}${query}`, `Message_Archive_${timestamp}.${ext}`);
                exportModal.classList.add('hidden');
            });
        });
    },

    setupDeleteModal() {
        const deleteConfirmModal = document.getElementById('deleteConfirmModal');
        let deletePendingIds = [];

        document.getElementById('hardDeleteSelectedBtn')?.addEventListener('click', () => {
            deletePendingIds = this.getSelectedIds();
            if (deletePendingIds.length === 0) return;

            const countSpan = document.getElementById('deleteConfirmCount');
            if (countSpan) countSpan.textContent = deletePendingIds.length;
            deleteConfirmModal.classList.remove('hidden');
        });

        document.getElementById('confirmHardDeleteBtn')?.addEventListener('click', safeAsync(async () => {
            if (deletePendingIds.length === 0) return;
            await api.hardDeleteTasks(deletePendingIds);
            const selectAll = document.getElementById('selectAllArchive');
            if (selectAll) selectAll.checked = false;
            this.updateActionsVisibility();
            this.fetch();
            deleteConfirmModal.classList.add('hidden');
        }, (error) => {
            renderer.showToast((I18N_DATA[state.currentLang]?.errorHardDelete || 'Error: ') + error.message, 'error');
        }));
    }
};