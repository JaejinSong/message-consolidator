import { state } from './state';
import { api } from './api';
import {
    showToast,
    renderReleaseNotes,
    renderProposals
} from './renderer';
import { safeAsync } from './utils';
import { TokenUsageCard } from './components/token-usage';

/**
 * @file modals.ts
 * @description UI module for handling modals (settings, release notes, etc.) and their logic.
 */

async function pollProposalJob(): Promise<{ proposals_created: number }> {
    const MAX = 72; // 6 minutes at 5s intervals
    for (let i = 0; i < MAX; i++) {
        await new Promise(r => setTimeout(r, 5000));
        const s = await api.getProposalJobStatus();
        if (s.status === 'done') return { proposals_created: s.proposals_created ?? 0 };
        if (s.status === 'error') throw new Error(s.error || 'Analysis failed');
    }
    throw new Error('Analysis timed out');
}

export interface ModalInterface {
    init(fetchMessagesCallback: () => void): void;
    attachGlobalCloseListeners(): void;
    setupReleaseNotesModal(): void;
    showOriginalModal(rawContent: string): void;
    fetchIdentityProposals(): Promise<void>;
    generateIdentityProposals(): Promise<void>;
    acceptIdentityProposal(groupId: string, canonicalName: string): Promise<void>;
    rejectIdentityProposal(groupId: string): Promise<void>;
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
    init(_fetchMessagesCallback: () => void) {
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
    cleanupModal(_modalId: string) {},

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

    fetchIdentityProposals: safeAsync(async function (this: ModalInterface) {
        const proposals = await api.fetchIdentityProposals();
        renderProposals(
            proposals,
            (groupId: string, name: string) => this.acceptIdentityProposal(groupId, name),
            (groupId: string) => this.rejectIdentityProposal(groupId)
        );
    }),

    generateIdentityProposals: safeAsync(async function (this: ModalInterface) {
        const btn = document.getElementById('generateProposalsBtn') as HTMLButtonElement;
        if (btn) { btn.disabled = true; btn.textContent = '분석 중...'; }
        try {
            try {
                await api.generateIdentityProposals();
            } catch (e: any) {
                if (e.status !== 409) throw e; // 409 = already running, join the ongoing poll
            }
            const result = await pollProposalJob();
            showToast(`제안 ${result.proposals_created}건 생성됨`, 'success');
            this.fetchIdentityProposals();
        } finally {
            if (btn) { btn.disabled = false; btn.textContent = 'AI 분석 실행'; }
        }
    }, { triggerAuthOverlay: true }),

    acceptIdentityProposal: safeAsync(async function (this: ModalInterface, groupId: string, canonicalName: string) {
        await api.acceptIdentityProposal(groupId, canonicalName);
        showToast(`"${canonicalName}"으로 통합됨`, 'success');
        this.fetchIdentityProposals();
    }, { triggerAuthOverlay: true }),

    rejectIdentityProposal: safeAsync(async function (this: ModalInterface, groupId: string) {
        await api.rejectIdentityProposal(groupId);
        this.fetchIdentityProposals();
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
                this.fetchIdentityProposals();
                document.getElementById('generateProposalsBtn')?.addEventListener('click', () => this.generateIdentityProposals());
            }
        });

    },

    /**
     * Opens alias mapping modal for a specific name.
     * @param name - Name to prepopulate mapping for.
     */
    openAliasMapping(_name: string) {
        const settingsModal = document.getElementById('settingsModal');
        if (settingsModal) {
            settingsModal.classList.remove('hidden');
            (settingsModal as HTMLElement).style.display = 'flex';
            (document.querySelector('[data-settings-tab="mappingsTab"]') as HTMLElement)?.click();
            this.fetchIdentityProposals();
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
