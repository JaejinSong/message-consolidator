import { state } from './state.js';
import { api } from './api.js';
import { renderer } from './renderer.js';
import { safeAsync } from './utils.js';

let onTasksChanged = null;

// --- [Utility] 모달 닫기 이벤트 공통 바인딩 ---
const bindModalCloseEvents = (modalId, closeBtnIds) => {
    const modal = document.getElementById(modalId);
    if (!modal) return modal;
    const close = () => modal.classList.add('hidden');
    closeBtnIds.forEach(id => document.getElementById(id)?.addEventListener('click', close));
    window.addEventListener('click', (e) => { if (e.target === modal) close(); });
    return modal;
};

export const modals = {
    init(fetchMessagesCallback) {
        onTasksChanged = fetchMessagesCallback;
        this.setupReleaseNotesModal();
        this.setupOriginalMessageModal();
        this.setupSettingsModal();
        this.setupGlobalHelpers();
    },

    // safeAsync 내부에 일반 함수(function)를 사용하여 modals 객체의 this를 유지합니다.
    fetchAliases: safeAsync(async function () {
        const aliases = await api.fetchAliases();
        state.userAliases = aliases;
        renderer.renderAliasList(state.userAliases, this.removeAlias.bind(this));
        if (onTasksChanged) onTasksChanged();
    }),

    addAlias: safeAsync(async function () {
        const input = document.getElementById('newAliasInput');
        const rawValue = input.value;
        if (!rawValue.trim()) return;

        const aliases = rawValue.split(',').map(a => a.trim()).filter(a => a);
        await Promise.all(aliases.map(a => api.addAlias(a)));
        input.value = '';
        this.fetchAliases();
    }),

    removeAlias: safeAsync(async function (alias) {
        await api.removeAlias(alias);
        this.fetchAliases();
    }),

    fetchTenantAliases: safeAsync(async function () {
        const aliases = await api.fetchTenantAliases();
        renderer.renderTenantAliasList(aliases, this.removeTenantAliasMapping.bind(this));
    }),

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
    }),

    removeTenantAliasMapping: safeAsync(async function (original) {
        await api.removeTenantAlias(original);
        this.fetchTenantAliases();
    }),

    fetchTokenUsage: safeAsync(async function () {
        const usage = await api.fetchTokenUsage();
        renderer.updateTokenBadge(usage);
    }),

    fetchContactMappings: safeAsync(async function () {
        const mappings = await api.fetchContactMappings();
        renderer.renderContactMappings(mappings, this.removeContactMapping.bind(this));
    }),

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
    }),

    removeContactMapping: safeAsync(async function (repName) {
        await api.removeContactMapping(repName);
        this.fetchContactMappings();
    }),

    setupReleaseNotesModal() {
        const showReleaseNotes = async () => {
            try {
                const data = await api.fetchReleaseNotes();
                if (data && data.content) {
                    renderer.renderReleaseNotes(data.content);
                }
            } catch (e) { console.error('Failed to fetch release notes:', e); }
        };

        document.getElementById('releaseNotesBtn')?.addEventListener('click', showReleaseNotes);
        document.getElementById('updatesBtn')?.addEventListener('click', showReleaseNotes);

        bindModalCloseEvents('releaseNotesModal', ['closeReleaseNotesBtn', 'confirmReleaseNotesBtn']);
    },

    setupOriginalMessageModal() {
        bindModalCloseEvents('originalMessageModal', ['closeOriginalBtn']);
    },

    setupSettingsModal() {
        document.getElementById('settingsBtn')?.addEventListener('click', () => {
            document.getElementById('settingsModal')?.classList.remove('hidden');
            renderer.renderAliasList(state.userAliases, this.removeAlias.bind(this));
            this.fetchTenantAliases();
            this.fetchContactMappings();
            this.fetchTokenUsage();
        });

        bindModalCloseEvents('settingsModal', ['closeSettingsBtn']);

        const bindEnter = (inputId, btnId, fn) => {
            document.getElementById(btnId)?.addEventListener('click', () => fn.call(this));
            document.getElementById(inputId)?.addEventListener('keypress', (e) => { if (e.key === 'Enter') fn.call(this); });
        };
        bindEnter('newAliasInput', 'addAliasBtn', this.addAlias);
        bindEnter('normPrimaryInput', 'addNormBtn', this.addTenantAliasMapping);
        bindEnter('contactAliasesInput', 'addContactBtn', this.addContactMapping);
    },

    setupGlobalHelpers() {
        window.removeContactMapping = this.removeContactMapping.bind(this);
        window.openAliasMapping = (name) => {
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
        };
    }
};