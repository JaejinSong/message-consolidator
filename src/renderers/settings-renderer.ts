import { escapeHTML } from '../utils';
import { Combobox } from '../components/combobox';
import { AccountItem, ComboboxInterface } from '../types';

/**
 * @file settings-renderer.ts
 * @description Renders setting components and manages account linking UI.
 */

export interface SettingsCompos {
    targetCombo: ComboboxInterface;
    masterCombo: ComboboxInterface;
}

/**
 * Renders the list of user aliases.
 */
export function renderAliasList(aliases: any, onRemove: (alias: string) => void): void {
    const container = document.getElementById('aliasList');
    if (!container) return;

    const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No aliases configured</p>';
        return;
    }

    container.innerHTML = list.map((alias: string) => `
        <div class="c-settings__item">
            <span>${escapeHTML(alias)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-alias-btn" data-alias="${escapeHTML(alias)}">&times;</button>
        </div>
    `).join('');

    container.querySelectorAll('.remove-alias-btn').forEach(btn => {
        const alias = (btn as HTMLElement).dataset.alias;
        if (alias) btn.addEventListener('click', () => onRemove(alias));
    });
}

/**
 * Renders the list of tenant aliases (Auto Name Correction).
 */
export function renderTenantAliasList(aliases: any, onRemove: (id: string) => void): void {
    const container = document.getElementById('normList');
    if (!container) return;

    const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);
    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No tenant aliases configured</p>';
        return;
    }

    container.innerHTML = list.map((item: any) => {
        if (!item || typeof item !== 'object') return '';

        // Requirement: Format ${alias.canonical_id} &rarr; ${alias.display_name}
        const cid = item.canonical_id || "";
        const dname = item.display_name || "";
        
        const displayStr = dname ? `${escapeHTML(cid)} &rarr; ${escapeHTML(dname)}` : escapeHTML(cid);
        // Requirement: Explicit integer conversion for data-id
        const numericId = Number(item.id || 0);

        return `
        <div class="c-settings__item">
            <span>${displayStr}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-tenant-alias-btn" data-id="${numericId}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.remove-tenant-alias-btn').forEach(btn => {
        const id = (btn as HTMLElement).dataset.id;
        if (id) btn.addEventListener('click', () => onRemove(id));
    });
}

/**
 * Renders contact mappings (Messenger Integration).
 */
export function renderContactMappings(mappings: any, onRemove: (id: string) => void): void {
    const container = document.getElementById('contactList');
    if (!container) return;

    const list = Array.isArray(mappings) ? mappings : (mappings?.mappings || mappings?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No contact mappings</p>';
        return;
    }

    container.innerHTML = list.map((m: any) => {
        if (!m || typeof m !== 'object') return '';

        const rep = m.display_name || m.rep_name || m.repName || m.name || "";
        const aliases = m.aliases || m.aliasNames || m.alias || m.source || "";
        const id = m.canonical_id || m.rep_name || m.repName || m.name || "";

        return `
        <div class="c-settings__item">
            <span>${escapeHTML(aliases)}</span>
            <span style="color: var(--text-dim);">→</span>
            <span style="font-weight: bold;">${escapeHTML(rep)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-mapping-btn" data-id="${escapeHTML(id)}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.remove-mapping-btn').forEach(btn => {
        const id = (btn as HTMLElement).dataset.id;
        if (id) btn.addEventListener('click', () => onRemove(id));
    });
}

/**
 * Renders the list of currently linked accounts.
 */
export function renderLinkedAccounts(links: any[], onUnlink: (id: string) => void): void {
    const container = document.getElementById('linkedAccountsList');
    if (!container) return;

    if (!links || links.length === 0) {
        container.innerHTML = '<p class="u-text-dim u-text-xs">No linked accounts found.</p>';
        return;
    }

    container.innerHTML = links.map(link => {
        if (!link || !link.target || !link.master) return '';

        // Requirement: use link.target.display_name || link.target.canonical_id
        const targetLabel = escapeHTML(link.target.display_name || link.target.canonical_id);
        const masterLabel = escapeHTML(link.master.display_name || link.master.canonical_id);
        
        // Requirement: Explicit integer conversion for data-id
        const numericTargetId = Number(link.target_id);

        return `
        <div class="c-settings__item">
            <span class="u-text-accent">${targetLabel}</span>
            <span class="u-mx-2 u-text-dim">→</span>
            <span class="u-font-bold">${masterLabel}</span>
            <button class="c-btn c-btn--ghost c-btn--icon u-ml-2 unlink-btn" data-id="${numericTargetId}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.unlink-btn').forEach(btn => {
        const id = (btn as HTMLElement).dataset.id;
        if (id) btn.addEventListener('click', () => onUnlink(id));
    });
}

/**
 * Initializes the account linking comboboxes.
 * @param searchFn - API search function.
 * @param onLink - Link action callback.
 */
export function initAccountLinkingCompos(
    searchFn: (q: string) => Promise<AccountItem[]>, 
    onLink: (targetId: number, masterId: number) => void
): SettingsCompos | undefined {
    const targetEl = document.getElementById('targetAccountCombobox');
    const masterEl = document.getElementById('masterAccountCombobox');
    const linkBtn = document.getElementById('linkAccountsBtn');

    if (!targetEl || !masterEl || !linkBtn) return undefined;

    const targetCombo = new Combobox(targetEl, {
        placeholder: 'Select target account...',
        searchFn: searchFn
    });

    const masterCombo = new Combobox(masterEl, {
        placeholder: 'Select master account...',
        searchFn: searchFn
    });

    linkBtn.onclick = () => {
        const target = targetCombo.getValue();
        const master = masterCombo.getValue();
        
        // Guard: ensure both are selected
        if (target && master) {
            // Why: Explicit integer conversion to satisfy backend requirements and ensure type safety.
            onLink(Number(target.id), Number(master.id));
            
            targetCombo.clear();
            masterCombo.clear();
        }
    };

    return { targetCombo, masterCombo };
}
