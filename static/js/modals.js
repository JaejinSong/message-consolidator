import { state } from './state.js';
import { api } from './api.js';
import { renderer } from './renderer.js';

let onTasksChanged = null;

export const modals = {
    init(fetchMessagesCallback) {
        onTasksChanged = fetchMessagesCallback;
        this.setupReleaseNotesModal();
        this.setupOriginalMessageModal();
        this.setupSettingsModal();
        this.setupGlobalHelpers();
    },

    async fetchAliases() {
        try {
            const aliases = await api.fetchAliases();
            state.userAliases = aliases;
            renderer.renderAliasList(state.userAliases, this.removeAlias.bind(this));
            if (onTasksChanged) onTasksChanged();
        } catch (e) { console.error(e); }
    },

    async addAlias() {
        const input = document.getElementById('newAliasInput');
        const rawValue = input.value;
        if (!rawValue.trim()) return;

        const aliases = rawValue.split(',').map(a => a.trim()).filter(a => a);
        try {
            await Promise.all(aliases.map(a => api.addAlias(a)));
            input.value = '';
            this.fetchAliases();
        } catch (e) { console.error(e); }
    },

    async removeAlias(alias) {
        try {
            await api.removeAlias(alias);
            this.fetchAliases();
        } catch (e) { console.error(e); }
    },

    async fetchTenantAliases() {
        try {
            const aliases = await api.fetchTenantAliases();
            renderer.renderTenantAliasList(aliases, this.removeTenantAliasMapping.bind(this));
        } catch (e) { console.error(e); }
    },

    async addTenantAliasMapping() {
        const origInput = document.getElementById('normOriginalInput');
        const primInput = document.getElementById('normPrimaryInput');
        const original = origInput.value.trim();
        const primary = primInput.value.trim();
        if (!original || !primary) return;
        try {
            await api.addTenantAlias(original, primary);
            origInput.value = '';
            primInput.value = '';
            this.fetchTenantAliases();
        } catch (e) { console.error(e); }
    },

    async removeTenantAliasMapping(original) {
        try {
            await api.removeTenantAlias(original);
            this.fetchTenantAliases();
        } catch (e) { console.error(e); }
    },

    async fetchTokenUsage() {
        try {
            const usage = await api.fetchTokenUsage();
            renderer.updateTokenBadge(usage);
        } catch (e) { console.error(e); }
    },

    async fetchContactMappings() {
        try {
            const mappings = await api.fetchContactMappings();
            renderer.renderContactMappings(mappings, this.removeContactMapping.bind(this));
        } catch (e) { console.error(e); }
    },

    async addContactMapping() {
        const repInput = document.getElementById('contactRepInput');
        const aliasInput = document.getElementById('contactAliasesInput');
        const repName = repInput.value.trim();
        const aliases = aliasInput.value.trim();
        if (!repName || !aliases) return;
        try {
            await api.addContactMapping(repName, aliases);
            repInput.value = '';
            aliasInput.value = '';
            this.fetchContactMappings();
        } catch (e) { console.error(e); }
    },

    async removeContactMapping(repName) {
        try {
            await api.removeContactMapping(repName);
            this.fetchContactMappings();
        } catch (e) { console.error(e); }
    },

    setupReleaseNotesModal() {
        const releaseNotesModal = document.getElementById('releaseNotesModal');
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
        document.getElementById('closeReleaseNotesBtn')?.addEventListener('click', () => releaseNotesModal.classList.add('hidden'));
        document.getElementById('confirmReleaseNotesBtn')?.addEventListener('click', () => releaseNotesModal.classList.add('hidden'));
        window.addEventListener('click', (e) => { if (e.target === releaseNotesModal) releaseNotesModal.classList.add('hidden'); });
    },

    setupOriginalMessageModal() {
        const originalModal = document.getElementById('originalMessageModal');
        document.getElementById('closeOriginalBtn')?.addEventListener('click', () => originalModal.classList.add('hidden'));
        window.addEventListener('click', (e) => { if (e.target === originalModal) originalModal.classList.add('hidden'); });
    },

    setupSettingsModal() {
        const settingsModal = document.getElementById('settingsModal');
        document.getElementById('settingsBtn')?.addEventListener('click', () => {
            settingsModal.classList.remove('hidden');
            renderer.renderAliasList(state.userAliases, this.removeAlias.bind(this));
            this.fetchTenantAliases();
            this.fetchContactMappings();
            this.fetchTokenUsage();
        });
        document.getElementById('closeSettingsBtn')?.addEventListener('click', () => settingsModal.classList.add('hidden'));
        window.addEventListener('click', (e) => { if (e.target === settingsModal) settingsModal.classList.add('hidden'); });

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