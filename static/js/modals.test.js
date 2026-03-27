import { describe, it, expect, beforeEach, vi } from 'vitest';
import { modals } from './modals.js';
import { api } from './api.js';

vi.mock('./api.js', () => ({
    api: {
        fetchAliases: vi.fn().mockResolvedValue([]),
        fetchTenantAliases: vi.fn().mockResolvedValue([]),
        fetchContactMappings: vi.fn().mockResolvedValue([]),
        fetchTokenUsage: vi.fn().mockResolvedValue({ total: 0 }),
        fetchReleaseNotes: vi.fn().mockResolvedValue({ content: 'test' })
    }
}));

describe('modals.js', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <button id="settingsBtn"></button>
            <div id="settingsModal" class="c-modal hidden">
                <button class="c-modal__close"></button>
            </div>
            <div id="exportModal" class="c-modal hidden"></div>
            <div id="deleteConfirmModal" class="c-modal hidden"></div>
            <div id="releaseNotesModal" class="c-modal hidden"></div>
        `;
        modals.init(() => {});
    });

    it('should show settings modal when settingsBtn is clicked', () => {
        const btn = document.getElementById('settingsBtn');
        const modal = document.getElementById('settingsModal');
        
        btn.click();
        expect(modal.classList.contains('hidden')).toBe(false);
        expect(api.fetchTenantAliases).toHaveBeenCalled();
    });

    it('should hide modal when close-btn is clicked (event delegation)', () => {
        const modal = document.getElementById('settingsModal');
        modal.classList.remove('hidden');
        
        const closeBtn = modal.querySelector('.c-modal__close');
        closeBtn.click();
        expect(modal.classList.contains('hidden')).toBe(true);
    });

    it('should hide modal when clicking outside (on the modal overlay)', () => {
        const modal = document.getElementById('settingsModal');
        modal.classList.remove('hidden');
        
        // Click on the modal itself (overlay)
        modal.click();
        expect(modal.classList.contains('hidden')).toBe(true);
    });
});
