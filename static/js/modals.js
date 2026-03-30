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
        let currentRnType = 'user';
        let currentRnLang = state.currentLang === 'ko' ? 'ko' : 'en';

        const fetchAndRenderReleaseNotes = async () => {
            const contentEl = document.getElementById('releaseNotesContent');
            if (contentEl) contentEl.innerHTML = '<div style="text-align:center; padding: 2rem;" class="u-text-dim">Loading...</div>';

            try {
                const data = await api.fetchReleaseNotes(currentRnType, currentRnLang);
                if (data && data.content) {
                    renderer.renderReleaseNotes(data.content);
                }
            } catch (e) {
                console.error('Failed to fetch release notes:', e);
                if (contentEl) contentEl.innerHTML = '<div style="color:var(--color-error); text-align:center; padding: 2rem;">Failed to load release notes.</div>';
            }
        };

        const showReleaseNotes = () => {
            // 앱 전역 언어 설정과 동기화 (한국어가 아니면 영문으로 Fallback)
            currentRnLang = state.currentLang === 'ko' ? 'ko' : 'en';

            // 탭 UI 활성화 상태 동기화
            document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(b => {
                b.classList.toggle('active', b.dataset.lang === currentRnLang);
            });

            document.getElementById('releaseNotesModal')?.classList.remove('hidden');
            fetchAndRenderReleaseNotes();
        };

        document.getElementById('releaseNotesBtn')?.addEventListener('click', showReleaseNotes);
        document.getElementById('updatesBtn')?.addEventListener('click', showReleaseNotes);

        // Type Tabs (User/Tech)
        document.querySelectorAll('#rnTypeTabs .c-tabs__btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                document.querySelectorAll('#rnTypeTabs .c-tabs__btn').forEach(b => b.classList.remove('active'));
                e.target.classList.add('active');
                currentRnType = e.target.dataset.type;
                fetchAndRenderReleaseNotes();
            });
        });

        // Language Tabs (KO/EN)
        document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(b => b.classList.remove('active'));
                e.target.classList.add('active');
                currentRnLang = e.target.dataset.lang;
                fetchAndRenderReleaseNotes();
            });
        });
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
     * Fetches and renders current account links.
     */
    fetchLinkedAccounts: safeAsync(async function () {
        const links = await api.fetchLinkedAccounts();
        renderer.renderLinkedAccounts(links, (id) => this.unlinkAccount(id));
    }),

    /**
     * Links two accounts.
     */
    linkAccounts: safeAsync(async function (targetId, masterId) {
        if (targetId === masterId) {
            renderer.showToast('자기 지신을 연결할 수 없습니다.', 'error');
            return;
        }
        await api.linkAccounts(targetId, masterId);
        renderer.showToast('계정이 성공적으로 연결되었습니다.', 'success');
        this.fetchLinkedAccounts();
    }, { triggerAuthOverlay: true }),

    /**
     * Unlinks an account.
     */
    unlinkAccount: safeAsync(async function (targetId) {
        if (!confirm('정말로 이 연결을 해제하시겠습니까?')) return;
        await api.unlinkAccount(targetId);
        renderer.showToast('연결이 해제되었습니다.', 'success');
        this.fetchLinkedAccounts();
    }, { triggerAuthOverlay: true }),

    /**
     * Sets up settings modal and tab logic.
     */
    setupSettingsModal() {
        let comboboxesInitialized = false;

        document.getElementById('settingsBtn')?.addEventListener('click', () => {
            document.getElementById('settingsModal')?.classList.remove('hidden');
            renderer.renderAliasList(state.userAliases, (alias) => this.removeAlias(alias));
            this.fetchTenantAliases();
            this.fetchContactMappings();
            this.fetchTokenUsage();
            this.fetchLinkedAccounts();

            // Initialize comboboxes only once
            if (!comboboxesInitialized) {
                renderer.initAccountLinkingCompos(
                    (q) => api.searchContacts(q),
                    (target, master) => this.linkAccounts(target, master)
                );
                comboboxesInitialized = true;
            }
        });

        const bindEnter = (inputId, btnId, fn) => {
            document.getElementById(btnId)?.addEventListener('click', () => fn.call(this));
            document.getElementById(inputId)?.addEventListener('keypress', (e) => { if (e.key === 'Enter') fn.call(this); });
        };
        bindEnter('newAliasInput', 'addAliasBtn', this.addAlias);
        bindEnter('normPrimaryInput', 'addNormBtn', this.addTenantAliasMapping);
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