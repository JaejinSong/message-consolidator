import { state } from './state.js';
import { api } from './api.js';
import { renderer } from './renderer.js';
import { safeAsync } from './utils.js';

/**
 * @file modals.js
 * @description UI module for handling modals (settings, release notes, etc.) and their logic.
 */

let onTasksChanged = null;

export const modals = {
    /**
     * Initializes the modals module.
     * @param {Function} fetchMessagesCallback - Callback to refresh messages.
     */
    init(fetchMessagesCallback) {
        onTasksChanged = fetchMessagesCallback;
        this.attachGlobalCloseListeners();
        this.setupReleaseNotesModal();
        this.setupSettingsModal();
        this.setupEventListeners();
    },

    /**
     * 이벤트 위임을 활용하여 모든 모달의 닫기 액션을 통합 관리합니다.
     */
    attachGlobalCloseListeners() {
        document.body.addEventListener('click', (e) => {
            // 1. '.c-modal__close' (X 버튼) 또는 'data-action="close-modal"' 속성을 가진 버튼 클릭 시

            if (e.target.closest('.c-modal__close') || e.target.closest('[data-action="close-modal"]')) {
                const modal = e.target.closest('.c-modal');
                if (modal) {
                    modal.classList.add('hidden');
                    console.log('[DEBUG] Modal Closed via Button:', modal.id);
                }
            }
            // 2. 모달 콘텐츠 바깥(어두운 배경 영역) 클릭 시 닫기
            else if (e.target.classList.contains('c-modal')) {
                e.target.classList.add('hidden');
                console.log('[DEBUG] Modal Closed via Backdrop:', e.target.id);
            }
        });
    },

    /**
     * Fetches and renders user aliases.
     */
    fetchAliases: safeAsync(async function () {
        const aliases = await api.fetchAliases();
        state.userAliases = aliases;
        renderer.renderAliasList(state.userAliases, this.removeAlias.bind(this));
        if (onTasksChanged) onTasksChanged();
    }),

    /**
     * Adds a new alias for the user.
     */
    addAlias: safeAsync(async function () {
        const input = document.getElementById('newAliasInput');
        const rawValue = input.value;
        if (!rawValue.trim()) return;

        const aliases = rawValue.split(',').map(a => a.trim()).filter(a => a);
        await Promise.all(aliases.map(a => api.addAlias(a)));
        input.value = '';
        this.fetchAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Removes an alias.
     * @param {string} alias - Alias to remove.
     */
    removeAlias: safeAsync(async function (alias) {
        await api.removeAlias(alias);
        this.fetchAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Fetches and renders tenant aliases.
     */
    fetchTenantAliases: safeAsync(async function () {
        const aliases = await api.fetchTenantAliases();
        renderer.renderTenantAliasList(aliases, this.removeTenantAliasMapping.bind(this));
    }),

    /**
     * Adds a new tenant alias mapping.
     */
    addTenantAliasMapping: safeAsync(async function () {
        const origInput = document.getElementById('normOriginalInput');
        const primInput = document.getElementById('normPrimaryInput');
        const original = origInput.value.trim();
        const primary = primInput.value.trim();
        if (!original || !primary) return;

        await api.addTenantAlias(original, primary);
        origInput.value = '';
        primInput.value = '';
        this.fetchTenantAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Removes a tenant alias mapping.
     * @param {string} original - Original name.
     */
    removeTenantAliasMapping: safeAsync(async function (original) {
        await api.removeTenantAlias(original);
        this.fetchTenantAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Fetches and updates token usage count.
     */
    fetchTokenUsage: safeAsync(async function () {
        const usage = await api.fetchTokenUsage();
        renderer.updateTokenBadge(usage);
    }),

    /**
     * Fetches and renders contact mappings.
     */
    fetchContactMappings: safeAsync(async function () {
        const mappings = await api.fetchContactMappings();
        renderer.renderContactMappings(mappings, (repName) => this.removeContactMapping(repName));
    }),

    /**
     * Adds a new contact mapping.
     */
    addContactMapping: safeAsync(async function () {
        const repInput = document.getElementById('contactRepInput');
        const aliasInput = document.getElementById('contactAliasesInput');
        const repName = repInput.value.trim();
        const aliases = aliasInput.value.trim();
        if (!repName || !aliases) return;

        await api.addContactMapping(repName, aliases);
        repInput.value = '';
        aliasInput.value = '';
        this.fetchContactMappings();
    }, { triggerAuthOverlay: true }),

    /**
     * Removes a contact mapping.
     * @param {string} repName - Representative name to remove.
     */
    removeContactMapping: safeAsync(async function (repName) {
        await api.removeContactMapping(repName);
        this.fetchContactMappings();
    }, { triggerAuthOverlay: true }),

    /**
     * Sets up release notes modal and triggers.
     */
    setupReleaseNotesModal() {
        const showReleaseNotes = async () => {
            try {
                const data = await api.fetchReleaseNotes();
                if (data && data.content) {
                    renderer.renderReleaseNotes(data.content);
                    document.getElementById('releaseNotesModal')?.classList.remove('hidden');
                }
            } catch (e) { console.error('Failed to fetch release notes:', e); }
        };

        document.getElementById('releaseNotesBtn')?.addEventListener('click', showReleaseNotes);
        document.getElementById('updatesBtn')?.addEventListener('click', showReleaseNotes);

    },

    /**
     * Shows the original message in a beautiful modal.
     * @param {string} rawContent - The original message content.
     */
    showOriginalModal(rawContent) {
        const modal = document.getElementById('originalMessageModal');
        const contentEl = document.getElementById('originalTextContent');
        if (modal && contentEl) {
            contentEl.textContent = rawContent;
            modal.classList.remove('hidden');
        }
    },

    /**
     * Sets up settings modal and tab logic.
     */
    setupSettingsModal() {
        document.getElementById('settingsBtn')?.addEventListener('click', () => {
            document.getElementById('settingsModal')?.classList.remove('hidden');
            renderer.renderAliasList(state.userAliases, (alias) => this.removeAlias(alias));
            this.fetchTenantAliases();
            this.fetchContactMappings();
            this.fetchTokenUsage();
        });

        const bindEnter = (inputId, btnId, fn) => {
            document.getElementById(btnId)?.addEventListener('click', () => fn.call(this));
            document.getElementById(inputId)?.addEventListener('keypress', (e) => { if (e.key === 'Enter') fn.call(this); });
        };
        bindEnter('newAliasInput', 'addAliasBtn', this.addAlias);
        bindEnter('normPrimaryInput', 'addNormBtn', this.addTenantAliasMapping);
        bindEnter('contactAliasesInput', 'addContactBtn', this.addContactMapping);
    },

    /**
     * Opens alias mapping modal for a specific name.
     * @param {string} name - Name to prepopulate mapping for.
     */
    openAliasMapping(name) {
        const settingsModal = document.getElementById('settingsModal');
        if (settingsModal) {
            settingsModal.classList.remove('hidden');
            document.querySelector('[data-settings-tab="mappingsTab"]')?.click();
            this.fetchTenantAliases();
            this.fetchContactMappings();
            const origInput = document.getElementById('normOriginalInput');
            const contactAliasInput = document.getElementById('contactAliasesInput');
            if (origInput) origInput.value = name;
            if (contactAliasInput) contactAliasInput.value = name;
            document.getElementById('normPrimaryInput')?.focus();
        }
    },

    /**
     * Sets up global event listeners to replace former window pollution.
     */
    setupEventListeners() {
        // Listen for alias mapping requests from other modules (like renderer)
        window.addEventListener('openAliasMapping', (e) => {
            if (e.detail && e.detail.name) {
                this.openAliasMapping(e.detail.name);
            }
        });
    }
};