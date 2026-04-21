import { state } from './state';
import { api } from './api';
import { 
    showToast, 
    renderAliasList, 
    renderTenantAliasList, 
    renderContactMappings, 
    renderReleaseNotes, 
    renderLinkedAccounts, 
    initAccountLinkingCompos 
} from './renderer';
import { safeAsync } from './utils';
import { SettingsCompos } from './renderers/settings-renderer';
import { TokenUsageCard } from './components/token-usage';

/**
 * @file modals.ts
 * @description UI module for handling modals (settings, release notes, etc.) and their logic.
 */

let onTasksChanged: (() => void) | null = null;
let settingsCompos: SettingsCompos | null = null;

export interface ModalInterface {
    init(fetchMessagesCallback: () => void): void;
    attachGlobalCloseListeners(): void;
    fetchAliases(): Promise<void>;
    addAlias(): Promise<void>;
    removeAlias(alias: string): Promise<void>;
    fetchTenantAliases(): Promise<void>;
    addTenantAliasMapping(): Promise<void>;
    removeTenantAliasMapping(original: string): Promise<void>;
    fetchContactMappings(): Promise<void>;
    addContactMapping(): Promise<void>;
    removeContactMapping(repName: string): Promise<void>;
    setupReleaseNotesModal(): void;
    showOriginalModal(rawContent: string): void;
    fetchLinkedAccounts(): Promise<void>;
    linkAccounts(targetId: string, masterId: string): Promise<void>;
    unlinkAccount(targetId: string): Promise<void>;
    setupSettingsModal(): void;
    cleanupModal(modalId: string): void;
    openAliasMapping(name: string): void;
    fetchTokenUsage(): Promise<void>;
    setupEventListeners(): void;
}

export const modals: any = {
    tokenCard: null as TokenUsageCard | null,

    /**
     * Initializes the modals module.
     * @param fetchMessagesCallback - Callback to refresh messages.
     */
    init(fetchMessagesCallback: () => void) {
        onTasksChanged = fetchMessagesCallback;
        this.attachGlobalCloseListeners();
        this.setupReleaseNotesModal();
        this.setupSettingsModal();
        this.tokenCard = new TokenUsageCard('settingsTokenUsageContainer');
        this.setupEventListeners();
    },

    /**
     * Unified management of modal close actions using event delegation.
     */
    attachGlobalCloseListeners() {
        document.body.addEventListener('click', (e: MouseEvent) => {
            const target = e.target as HTMLElement;
            
            // 1. Click on '.c-modal__close' (X button) or buttons with 'data-action="close-modal"'
            if (target.closest('.c-modal__close') || target.closest('[data-action="close-modal"]')) {
                const modal = target.closest('.c-modal');
                if (modal) {
                    this.cleanupModal(modal.id);
                    modal.classList.add('hidden');
                    (modal as HTMLElement).style.display = 'none';
                    console.debug('[MODAL] Closed via button:', modal.id);
                }
            }
            // 2. Click outside modal content (backdrop area)
            else if (target.classList.contains('c-modal')) {
                this.cleanupModal(target.id);
                target.classList.add('hidden');
                (target as HTMLElement).style.display = 'none';
                console.debug('[MODAL] Closed via backdrop:', target.id);
            }
        });
    },

    /**
     * Internal cleanup logic for specific modals.
     */
    cleanupModal(modalId: string) {
        if (modalId === 'settingsModal' && settingsCompos) {
            console.debug('[MODAL] Cleaning up settings comboboxes');
            settingsCompos.targetCombo.destroy();
            settingsCompos.masterCombo.destroy();
            settingsCompos = null;
        }
    },

    /**
     * Fetches and renders user aliases.
     */
    fetchAliases: safeAsync(async function (this: ModalInterface) {
        const aliases = await api.fetchAliases();
        (state as any).userAliases = aliases;
        renderAliasList((state as any).userAliases, this.removeAlias.bind(this));
        if (onTasksChanged) onTasksChanged();
    }),

    /**
     * Adds a new alias for the user.
     */
    addAlias: safeAsync(async function (this: ModalInterface) {
        const input = document.getElementById('newAliasInput') as HTMLInputElement;
        if (!input) return;
        const rawValue = input.value;
        if (!rawValue.trim()) return;

        const aliases = rawValue.split(',').map(a => a.trim()).filter(a => a);
        await Promise.all(aliases.map(a => api.addAlias(a)));
        input.value = '';
        this.fetchAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Removes an alias.
     * @param alias - Alias to remove.
     */
    removeAlias: safeAsync(async function (this: ModalInterface, alias: string) {
        await api.removeAlias(alias);
        this.fetchAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Fetches and renders tenant aliases.
     */
    fetchTenantAliases: safeAsync(async function (this: ModalInterface) {
        const aliases = await api.fetchTenantAliases();
        renderTenantAliasList(aliases, this.removeTenantAliasMapping.bind(this));
    }),

    /**
     * Adds a new tenant alias mapping.
     */
    addTenantAliasMapping: safeAsync(async function (this: ModalInterface) {
        const origInput = document.getElementById('normOriginalInput') as HTMLInputElement;
        const primInput = document.getElementById('normPrimaryInput') as HTMLInputElement;
        if (!origInput || !primInput) return;

        const original = origInput.value.trim();
        const primary = primInput.value.trim();
        if (!original || !primary) return;

        await api.addTenantAlias([original], primary);
        origInput.value = '';
        primInput.value = '';
        this.fetchTenantAliases();
    }, { triggerAuthOverlay: true }),

    /**
     * Removes a tenant alias mapping.
     * @param original - Original name.
     */
    removeTenantAliasMapping: safeAsync(async function (this: ModalInterface, original: string) {
        await api.removeTenantAlias(original);
        this.fetchTenantAliases();
    }, { triggerAuthOverlay: true }),


    /**
     * Fetches and renders contact mappings.
     */
    fetchContactMappings: safeAsync(async function (this: ModalInterface) {
        const mappings = await api.fetchContactMappings();
        renderContactMappings(mappings, (repName: string) => this.removeContactMapping(repName));
    }, { triggerAuthOverlay: true }),

    /**
     * Adds a new contact mapping.
     */
    addContactMapping: safeAsync(async function (this: ModalInterface) {
        const repInput = document.getElementById('contactRepInput') as HTMLInputElement;
        const aliasInput = document.getElementById('contactAliasesInput') as HTMLInputElement;
        if (!repInput || !aliasInput) return;

        const repName = repInput.value.trim();
        const aliases = aliasInput.value.trim();
        if (!repName || !aliases) return;

        try {
            await api.addContactMapping(repName, aliases.split(',').map(s => s.trim()).filter(Boolean));
            repInput.value = '';
            aliasInput.value = '';
            this.fetchContactMappings();
            showToast('Contact mapping added successfully', 'success');
        } catch (e: any) {
            if (e.status === 409) {
            showToast('Mapping already exists for this identity', 'error');
                return;
            }
            throw e;
        }
    }, { triggerAuthOverlay: true }),

    /**
     * Removes a contact mapping.
     * @param repName - Representative name to remove.
     */
    removeContactMapping: safeAsync(async function (this: ModalInterface, repName: string) {
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
                    renderReleaseNotes(data.content);
                }
            } catch (e) {
                console.error('Failed to fetch release notes:', e);
                if (contentEl) contentEl.innerHTML = '<div style="color:var(--color-error); text-align:center; padding: 2rem;">Failed to load release notes.</div>';
            }
        };

        const showReleaseNotes = () => {
            currentRnLang = state.currentLang === 'ko' ? 'ko' : 'en';
            document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(b => {
                const btn = b as HTMLElement;
                btn.classList.toggle('active', btn.dataset.lang === currentRnLang);
            });

            const modal = document.getElementById('releaseNotesModal');
            if (modal) {
                modal.classList.remove('hidden');
                modal.style.display = 'flex';
                fetchAndRenderReleaseNotes();
            }
        };

        document.getElementById('releaseNotesBtn')?.addEventListener('click', showReleaseNotes);
        document.getElementById('updatesBtn')?.addEventListener('click', showReleaseNotes);

        document.querySelectorAll('#rnTypeTabs .c-tabs__btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;
                document.querySelectorAll('#rnTypeTabs .c-tabs__btn').forEach(b => b.classList.remove('active'));
                target.classList.add('active');
                currentRnType = target.dataset.type || 'user';
                fetchAndRenderReleaseNotes();
            });
        });

        document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;
                document.querySelectorAll('#rnLangTabs .c-tabs__btn').forEach(b => b.classList.remove('active'));
                target.classList.add('active');
                currentRnLang = target.dataset.lang || 'en';
                fetchAndRenderReleaseNotes();
            });
        });
    },

    /**
     * Shows the original message in a robust modal.
     * @param rawContent - The original message content.
     */
    showOriginalModal(rawContent: string) {
        const modal = document.getElementById('originalMessageModal');
        const contentEl = document.getElementById('originalTextContent');
        
        console.debug('[MODAL] showOriginalModal triggered', { 
            hasContent: !!rawContent, 
            contentLength: rawContent?.length,
            modalFound: !!modal,
            contentElFound: !!contentEl 
        });

        if (modal && contentEl) {
            contentEl.textContent = rawContent;
            modal.classList.remove('hidden');
            (modal as HTMLElement).style.display = 'flex';
            console.debug('[MODAL] originalMessageModal is now visible');
        } else {
            console.error('[MODAL] showOriginalModal failed: Missing DOM elements!', { modal, contentEl });
        }
    },

    /**
     * Fetches and renders current account links.
     */
    fetchLinkedAccounts: safeAsync(async function (this: ModalInterface) {
        const links = await api.fetchLinkedAccounts();
        renderLinkedAccounts(links, (id: string) => this.unlinkAccount(id));
    }),

    /**
     * Links two accounts.
     */
    linkAccounts: safeAsync(async function (this: ModalInterface, targetId: string, masterId: string) {
        if (targetId === masterId) {
            showToast('자기 자신을 연결할 수 없습니다.', 'error');
            return;
        }
        await api.linkAccounts(targetId, masterId);
        showToast('계정이 성공적으로 연결되었습니다.', 'success');
        this.fetchLinkedAccounts();
    }, { triggerAuthOverlay: true }),

    /**
     * Unlinks an account.
     */
    unlinkAccount: safeAsync(async function (this: ModalInterface, targetId: string) {
        if (!confirm('정말로 이 연결을 해제하시겠습니까?')) return;
        await api.unlinkAccount(targetId);
        showToast('연결이 해제되었습니다.', 'success');
        this.fetchLinkedAccounts();
    }, { triggerAuthOverlay: true }),

    /**
     * Sets up settings modal and tab logic.
     */
    setupSettingsModal() {
        const settingsBtn = document.getElementById('settingsBtn');
        console.debug('[MODAL] setupSettingsModal - settingsBtn found:', !!settingsBtn);

        settingsBtn?.addEventListener('click', (e) => {
            console.debug('[MODAL] Settings button clicked', e);
            const modal = document.getElementById('settingsModal');
            if (modal) {
                modal.classList.remove('hidden');
                (modal as HTMLElement).style.display = 'flex';
                renderAliasList((state as any).userAliases || [], (alias: string) => this.removeAlias(alias));
                this.fetchTenantAliases();
                this.fetchContactMappings();
                this.fetchLinkedAccounts();

                // Why: Always re-initialize to ensure fresh DOM state, cleanup handled by global close listener.
                if (settingsCompos) {
                    settingsCompos.targetCombo.destroy();
                    settingsCompos.masterCombo.destroy();
                }
                
                const compos = initAccountLinkingCompos(
                    (q: string) => api.searchContacts(q),
                    (target: number, master: number) => this.linkAccounts(String(target), String(master))
                );

                if (compos) {
                    settingsCompos = compos;
                }
            }
        });

        const bindEnter = (inputId: string, btnId: string, fn: Function) => {
            document.getElementById(btnId)?.addEventListener('click', () => fn.call(this));
            document.getElementById(inputId)?.addEventListener('keypress', (e) => { 
                if ((e as KeyboardEvent).key === 'Enter') fn.call(this); 
            });
        };
        bindEnter('newAliasInput', 'addAliasBtn', this.addAlias);
        bindEnter('normPrimaryInput', 'addNormBtn', this.addTenantAliasMapping);
    },

    /**
     * Opens alias mapping modal for a specific name.
     * @param name - Name to prepopulate mapping for.
     */
    openAliasMapping(name: string) {
        const settingsModal = document.getElementById('settingsModal');
        if (settingsModal) {
            settingsModal.classList.remove('hidden');
            (settingsModal as HTMLElement).style.display = 'flex';
            (document.querySelector('[data-settings-tab="mappingsTab"]') as HTMLElement)?.click();
            this.fetchTenantAliases();
            this.fetchContactMappings();
            const origInput = document.getElementById('normOriginalInput') as HTMLInputElement;
            const contactAliasInput = document.getElementById('contactAliasesInput') as HTMLInputElement;
            if (origInput) origInput.value = name;
            if (contactAliasInput) contactAliasInput.value = name;
            (document.getElementById('normPrimaryInput') as HTMLElement)?.focus();
        }
    },

    /**
     * Sets up global event listeners.
     */
    setupEventListeners() {
        window.addEventListener('openAliasMapping', (e: any) => {
            if (e.detail && e.detail.name) {
                this.openAliasMapping(e.detail.name);
            }
        });
    },

    /**
     * Fetches and renders token usage.
     */
    fetchTokenUsage: safeAsync(async function (this: any) {
        const usage = await api.fetchTokenUsage();
        if (usage && this.tokenCard) {
            this.tokenCard.render(usage);
        }
    })
};
