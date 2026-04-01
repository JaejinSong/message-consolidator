/**
 * @file renderer.js
 * @description Aggregator for modular UI renderers.
 */

import { 
    renderMessages, 
    createCardElement, 
    renderArchive,
    renderEmptyGrid
} from './renderers/task-renderer.ts';

import { 
    updateServiceStatusUI, 
    updateSlackStatus, 
    updateWhatsAppStatus, 
    updateGmailStatus,
    showWaModal,
    showGmailModal,
    bindGetQRBtn,
    updateWhatsAppQR,
    updateQRTimer,
    bindGmailStatus,
    bindWhatsAppStatus
} from './renderers/status-renderer.js';

import { updateUserProfile } from './renderers/profile-renderer.js';

import { 
    renderAliasList, 
    renderTenantAliasList, 
    renderContactMappings,
    renderLinkedAccounts,
    initAccountLinkingCompos
} from './renderers/settings-renderer.js';

import { 
    triggerXPAnimation, 
    triggerConfetti, 
    showToast, 
    renderReleaseNotes, 
    setScanLoading, 
    setTheme, 
    bindThemeToggle 
} from './renderers/ui-effects.js';

/**
 * Legacy renderer object for backward compatibility with app.js and other modules.
 */
export const renderer = {
    // Task Rendering
    renderMessages,
    createCardElement,
    renderArchive,
    renderEmptyGrid,

    // Service Status
    updateServiceStatusUI,
    updateSlackStatus,
    updateWhatsAppStatus,
    updateGmailStatus,
    showWaModal,
    showGmailModal,
    bindGetQRBtn,
    updateWhatsAppQR,
    updateQRTimer,
    bindGmailStatus,
    bindWhatsAppStatus,

    // Profile & Settings
    updateUserProfile,
    renderAliasList,
    renderTenantAliasList,
    renderContactMappings,
    renderLinkedAccounts,
    initAccountLinkingCompos,

    // UI Effects
    triggerXPAnimation,
    triggerConfetti,
    showToast,
    renderReleaseNotes,
    setScanLoading,
    setTheme,
    bindThemeToggle,

    // Miscellany
    updateTokenBadge: (usage) => {
        // This was a bit involved, so I kept it or can move to its own module if needed.
        // For now, I'll define it here or in status-renderer.
        const badge = document.getElementById('tokenUsageBadge');
        if (!badge) return;

        const data = usage || {};
        const todayTotal = data.todayTotal || 0;
        const monthTotal = data.monthTotal || data.monthlyTotal || 0;
        const monthCost = data.monthCost || 0;

        badge.classList.remove('hidden');
        badge.textContent = `Day: ${todayTotal.toLocaleString()} / Month: ${monthTotal.toLocaleString()} / Est: $${monthCost.toFixed(2)}`;

        const todayPrompt = data.todayPrompt || data.dailyPrompt || 0;
        const todayComp = data.todayCompletion || data.dailyCompletion || 0;
        const tooltipText = `[오늘] 입력: ${todayPrompt.toLocaleString()} / 출력: ${todayComp.toLocaleString()}\n[이번 달] 총합: ${monthTotal.toLocaleString()}\n[예상 비용] $${monthCost.toFixed(4)}`;
        badge.setAttribute('title', tooltipText);

        badge.style.transform = 'scale(1.02)';
        setTimeout(() => badge.style.transform = 'scale(1)', 200);
    },

    bindScanBtn: (onClick) => {
        document.getElementById('scanBtn')?.addEventListener('click', onClick);
    },

    bindGlobalClicks: (handlers) => {
        document.body.addEventListener('click', (e) => {
            if (e.target && e.target.closest('#buyFreezeBtn')) {
                if (handlers.onBuyFreeze) handlers.onBuyFreeze();
            }
        });
    }
};

// Re-export individual functions for modern ESM usage
export {
    renderMessages,
    createCardElement,
    renderArchive,
    renderEmptyGrid,
    updateServiceStatusUI,
    updateSlackStatus,
    updateWhatsAppStatus,
    updateGmailStatus,
    updateUserProfile,
    renderAliasList,
    renderTenantAliasList,
    renderContactMappings,
    renderLinkedAccounts,
    initAccountLinkingCompos,
    triggerXPAnimation,
    triggerConfetti,
    showToast,
    renderReleaseNotes,
    setScanLoading,
    setTheme,
    bindThemeToggle
};
