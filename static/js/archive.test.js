import { describe, it, expect, vi, beforeEach } from 'vitest';
import { archive } from './archive.js';
import { state } from './state.js';
import { api } from './api.js';

vi.mock('./api.js', () => ({
    api: {
        fetchArchive: vi.fn(),
        fetchArchiveCount: vi.fn(),
        restoreTasks: vi.fn(),
        hardDeleteTasks: vi.fn()
    }
}));

describe('archive.js', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="archiveSection" class="hidden">
                <input type="checkbox" id="selectAllArchive">
                <div id="archiveBody">
                    <input type="checkbox" class="archive-check" data-id="1">
                    <input type="checkbox" class="archive-check" data-id="2">
                </div>
                <button id="restoreSelectedBtn" style="display: none;"></button>
                <button id="hardDeleteSelectedBtn" style="display: none;"></button>
                <div id="archivePageInfo"></div>
                <button id="prevArchivePage"></button>
                <button id="nextArchivePage"></button>
                <div id="archiveLoading"></div>
            </div>
        `;
        state.archivePage = 1;
        state.archiveLimit = 20;
        state.currentLang = 'ko';
    });

    it('should update pagination UI correctly', () => {
        state.archiveTotalCount = 50; // 3 pages (20 + 20 + 10)
        state.archivePage = 2;
        archive.updatePaginationUI();
        
        const info = document.getElementById('archivePageInfo');
        expect(info.textContent).toContain('2 / 3');
        expect(info.textContent).toContain('50');

        expect(document.getElementById('prevArchivePage').disabled).toBe(false);
        expect(document.getElementById('nextArchivePage').disabled).toBe(false);
    });

    it('should disable next button on last page', () => {
        state.archiveTotalCount = 20;
        state.archivePage = 1;
        archive.updatePaginationUI();
        expect(document.getElementById('nextArchivePage').disabled).toBe(true);
    });

    it('should toggle action buttons based on selection', () => {
        const checks = document.querySelectorAll('.archive-check');
        checks[0].checked = true;
        
        archive.updateActionsVisibility();
        expect(document.getElementById('restoreSelectedBtn').style.display).toBe('inline-block');
        
        checks[0].checked = false;
        archive.updateActionsVisibility();
        expect(document.getElementById('restoreSelectedBtn').style.display).toBe('none');
    });

    it('should collect selected IDs', () => {
        const checks = document.querySelectorAll('.archive-check');
        checks[0].checked = true;
        checks[1].checked = true;
        expect(archive.getSelectedIds()).toEqual([1, 2]);
    });

    it('should update state.archiveStatus when tabs are clicked', async () => {
        api.fetchArchive.mockResolvedValue({ total: 0, messages: [] });
        document.body.innerHTML += `
            <div id="archiveSection">
                <button class="tab-btn" data-tab="archiveAllTab" id="archiveAllTab"></button>
                <button class="tab-btn" data-tab="archiveDoneTab" id="archiveDoneTab"></button>
                <button class="tab-btn" data-tab="archiveTrashTab" id="archiveTrashTab"></button>
            </div>
        `;
        archive.setupEventListeners();
        
        const doneTab = document.getElementById('archiveDoneTab');
        doneTab.click();
        expect(state.archiveStatus).toBe('done');
        expect(state.archivePage).toBe(1);

        const trashTab = document.getElementById('archiveTrashTab');
        trashTab.click();
        expect(state.archiveStatus).toBe('trash');

        const allTab = document.getElementById('archiveAllTab');
        allTab.click();
        expect(state.archiveStatus).toBe('all');
    });

    it('should fetch and update state', async () => {
        api.fetchArchive.mockResolvedValue({ total: 100, messages: [] });
        await archive.fetch();
        expect(state.archiveTotalCount).toBe(100);
        expect(api.fetchArchive).toHaveBeenCalled();
    });
});
