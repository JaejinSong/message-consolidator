import { state } from './state';
import { I18N_DATA } from './locales';
import { api } from './api';
import { renderArchive, showToast } from './renderer';
import { safeAsync } from './utils';

let onTasksChangedCallback: (() => void) | null = null;

interface ArchiveModule {
    init: (fetchMessagesCallback: () => void) => void;
    isVisible: () => boolean;
    onShow: () => void;
    fetch: () => Promise<void>;
    updatePaginationUI: () => void;
    updateActionsVisibility: () => void;
    getSelectedIds: () => number[];
    setupEventListeners: () => void;
    setupExportModal: () => void;
    setupDeleteModal: () => void;
}

export const archive: ArchiveModule = {
    init(fetchMessagesCallback: () => void) {
        onTasksChangedCallback = fetchMessagesCallback;
        this.setupEventListeners();
    },

    isVisible(): boolean {
        const section = document.getElementById('archiveSection');
        return section ? !section.classList.contains('hidden') : false;
    },

    onShow() {
        const selectAll = document.getElementById('selectAllArchive') as HTMLInputElement;
        if (selectAll) selectAll.checked = false;
        this.updateActionsVisibility();
        this.fetch();
    },

    fetch: safeAsync(async function (this: typeof archive) {
        const loader = document.getElementById('archiveLoading');
        if (loader) loader.classList.add('active');
        try {
            const params = {
                q: state.archiveSearch,
                limit: state.archiveLimit,
                offset: (state.archivePage - 1) * state.archiveLimit,
                lang: state.currentLang,
                sort: state.archiveSort,
                order: state.archiveOrder,
                status: state.archiveStatus || 'all'
            };
            const data = await api.fetchArchive(params);
            state.archiveTotalCount = data.total;

            // Only update the 'All' tab badge if we are actually viewing 'all'
            if (params.status === 'all') {
                const archiveCountEl = document.getElementById('archiveCount');
                if (archiveCountEl) archiveCountEl.textContent = data.total.toString();
            }

            renderArchive(data.messages);
            this.updatePaginationUI();
        } finally {
            if (loader) loader.classList.remove('active');
        }
    }, { triggerAuthOverlay: true }),

    updatePaginationUI() {
        const totalPages = Math.ceil(state.archiveTotalCount / state.archiveLimit) || 1;
        const pageInfo = document.getElementById('archivePageInfo');
        const i18n = (I18N_DATA as any)[state.currentLang];

        if (pageInfo && i18n) {
            let text = i18n.archivePageInfo || `Page {page} / {totalPages} (Total: {totalCount})`;
            text = text.replace('{page}', state.archivePage.toString())
                .replace('{totalPages}', totalPages.toString())
                .replace('{totalCount}', state.archiveTotalCount.toString());
            pageInfo.textContent = text;
        }

        const prevBtn = document.getElementById('prevArchivePage') as HTMLButtonElement;
        const nextBtn = document.getElementById('nextArchivePage') as HTMLButtonElement;
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

    getSelectedIds(): number[] {
        return Array.from(document.querySelectorAll('.archive-check:checked')).map(cb => parseInt((cb as HTMLElement).getAttribute('data-id') || '0'));
    },

    setupEventListeners() {
        document.getElementById('selectAllArchive')?.addEventListener('change', (e) => {
            const checked = (e.target as HTMLInputElement).checked;
            document.querySelectorAll('.archive-check').forEach(cb => (cb as HTMLInputElement).checked = checked);
            this.updateActionsVisibility();
        });

        document.getElementById('archiveBody')?.addEventListener('change', (e) => {
            if ((e.target as HTMLElement).classList.contains('archive-check')) {
                this.updateActionsVisibility();
            }
        });

        let searchTimeout: any;
        document.getElementById('archiveSearchInput')?.addEventListener('input', (e) => {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                state.archiveSearch = (e.target as HTMLInputElement).value;
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
            const selectAll = document.getElementById('selectAllArchive') as HTMLInputElement;
            if (selectAll) selectAll.checked = false;
            this.updateActionsVisibility();
            this.fetch();
            if (onTasksChangedCallback) onTasksChangedCallback();
        }, { 
            onError: (e: any) => {
                showToast(((I18N_DATA as any)[state.currentLang]?.errorRestore || 'Error: ') + e.message, 'error');
            }, 
            triggerAuthOverlay: true 
        }));

        // Sorting
        const triggerArchiveSort = (field: string) => {
            if (state.archiveSort === field) {
                state.archiveOrder = state.archiveOrder === 'ASC' ? 'DESC' : 'ASC';
            } else {
                state.archiveSort = field;
                state.archiveOrder = 'DESC';
            }
            state.archivePage = 1;
            this.fetch();
        };

        const sortHeaders: Record<string, string> = {
            'ahSource': 'source', 'ahRoom': 'room', 'ahTask': 'task',
            'ahRequester': 'requester', 'ahAssignee': 'assignee',
            'ahTime': 'time', 'ahCompletedAt': 'completed_at'
        };
        Object.entries(sortHeaders).forEach(([id, field]) => {
            document.getElementById(id)?.addEventListener('click', () => triggerArchiveSort(field));
        });

        // 보관함 2단계 탭 바인딩 (전체 / 완료된 업무 / 병합한 업무 / 취소한 업무)
        const archiveTabs = document.querySelectorAll('#archiveSection .tab-btn') as NodeListOf<HTMLButtonElement>;
        archiveTabs.forEach(tab => {
            tab.addEventListener('click', (e) => {
                archiveTabs.forEach(btn => btn.classList.remove('active'));
                const currentTarget = e.currentTarget as HTMLButtonElement;
                currentTarget.classList.add('active');

                const target = currentTarget.getAttribute('data-tab');
                if (target === 'archiveDoneTab') {
                    state.archiveStatus = 'done';
                } else if (target === 'archiveCanceledTab') {
                    state.archiveStatus = 'canceled';
                } else if (target === 'archiveMergedTab') {
                    state.archiveStatus = 'merged';
                } else {
                    state.archiveStatus = 'all';
                }

                state.archivePage = 1;
                this.fetch();
            });
        });

        this.setupExportModal();
        this.setupDeleteModal();
    },

    setupExportModal() {
        const exportModal = document.getElementById('exportModal');
        document.getElementById('openExportModalBtn')?.addEventListener('click', safeAsync(async () => {
            const currentStatus = state.archiveStatus || 'all';
            const countData = await api.fetchArchiveCount(state.archiveSearch, currentStatus);
            const countEl = document.getElementById('exportCount');
            if (countEl) countEl.textContent = countData.count.toString();
            if (exportModal) exportModal.classList.remove('hidden');
        }, { 
            onError: (e: any) => {
                showToast(((I18N_DATA as any)[state.currentLang]?.errorArchiveCount || 'Error: ') + e.message, 'error');
            }, 
            triggerAuthOverlay: true 
        }));

        const downloadFile = (url: string, defaultFilename: string) => {
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
                const query = new URLSearchParams();
                if (state.archiveSearch) query.set('q', state.archiveSearch);
                query.set('status', state.archiveStatus || 'all');

                const now = new Date();
                const pad = (n: number) => String(n).padStart(2, '0');
                const timestamp = `${now.getFullYear()}_${pad(now.getMonth() + 1)}_${pad(now.getDate())}_${pad(now.getHours())}_${pad(now.getMinutes())}`;
                downloadFile(`${path}?${query.toString()}`, `Message_Archive_${timestamp}.${ext}`);
                if (exportModal) exportModal.classList.add('hidden');
            });
        });
    },

    setupDeleteModal() {
        const deleteConfirmModal = document.getElementById('deleteConfirmModal');
        let deletePendingIds: number[] = [];

        document.getElementById('hardDeleteSelectedBtn')?.addEventListener('click', () => {
            deletePendingIds = this.getSelectedIds();
            if (deletePendingIds.length === 0) return;

            const countSpan = document.getElementById('deleteConfirmCount');
            if (countSpan) countSpan.textContent = deletePendingIds.length.toString();
            if (deleteConfirmModal) deleteConfirmModal.classList.remove('hidden');
        });

        document.getElementById('confirmHardDeleteBtn')?.addEventListener('click', safeAsync(async () => {
            if (deletePendingIds.length === 0) return;
            await api.hardDeleteTasks(deletePendingIds);
            const selectAll = document.getElementById('selectAllArchive') as HTMLInputElement;
            if (selectAll) selectAll.checked = false;
            this.updateActionsVisibility();
            this.fetch();
            if (deleteConfirmModal) deleteConfirmModal.classList.add('hidden');
        }, { 
            onError: (error: any) => {
                showToast(((I18N_DATA as any)[state.currentLang]?.errorHardDelete || 'Error: ') + error.message, 'error');
            }, 
            triggerAuthOverlay: true 
        }));
    }
};
