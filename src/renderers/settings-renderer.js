import { escapeHTML } from '../utils.js';

/**
 * Renders the list of user aliases in settings.
 */
export function renderAliasList(aliases, onRemove) {
    const container = document.getElementById('aliasList');
    if (!container) return;

    const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No aliases configured</p>';
        return;
    }

    container.innerHTML = list.map(alias => `
        <div class="c-settings__item">
            <span>${escapeHTML(alias)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-alias-btn" data-alias="${escapeHTML(alias)}">&times;</button>
        </div>
    `).join('');

    container.querySelectorAll('.remove-alias-btn').forEach(btn => {
        btn.addEventListener('click', () => onRemove(btn.dataset.alias));
    });
}

/**
 * Renders the list of tenant aliases in settings (Auto Name Correction).
 */
export function renderTenantAliasList(aliases, onRemove) {
    const container = document.getElementById('normList');
    if (!container) return;

    const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No tenant aliases configured</p>';
        return;
    }

    container.innerHTML = list.map(item => {
        // Why: Defensive type check to prevent [object Object] rendering.
        if (!item || typeof item !== 'object') return '';

        const orig = item.aliases || item.original_name || item.original || "";
        const prim = item.display_name || item.primary_name || item.primary || "";
        const id = item.canonical_id || item.original || "";

        const displayStr = prim ? `${escapeHTML(orig)} → ${escapeHTML(prim)}` : escapeHTML(orig);

        return `
        <div class="c-settings__item">
            <span>${displayStr}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-tenant-alias-btn" data-id="${escapeHTML(id)}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.remove-tenant-alias-btn').forEach(btn => {
        btn.addEventListener('click', () => onRemove(btn.dataset.id));
    });
}

/**
 * Renders contact mappings in settings (Messenger Integration).
 */
export function renderContactMappings(mappings, onRemove) {
    const container = document.getElementById('contactList');
    if (!container) return;

    const list = Array.isArray(mappings) ? mappings : (mappings?.mappings || mappings?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No contact mappings</p>';
        return;
    }

    container.innerHTML = list.map(m => {
        // Why: Defensive type check and fallback to empty string to ensure UI integrity.
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
        btn.addEventListener('click', () => onRemove(btn.dataset.id));
    });
}

/**
 * Renders the list of currently linked accounts.
 */
export function renderLinkedAccounts(links, onUnlink) {
    const container = document.getElementById('linkedAccountsList');
    if (!container) return;

    if (!links || links.length === 0) {
        container.innerHTML = '<p class="u-text-dim u-text-xs">No linked accounts found.</p>';
        return;
    }

    container.innerHTML = links.map(link => `
        <div class="c-settings__item">
            <span class="u-text-accent">${escapeHTML(link.target_display_name || link.target_canonical_id)}</span>
            <span class="u-mx-2 u-text-dim">→</span>
            <span class="u-font-bold">${escapeHTML(link.master_display_name || link.master_canonical_id)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon u-ml-2 unlink-btn" data-id="${link.target_id}">&times;</button>
        </div>
    `).join('');

    container.querySelectorAll('.unlink-btn').forEach(btn => {
        btn.addEventListener('click', () => onUnlink(btn.dataset.id));
    });
}

/**
 * Initializes the account linking comboboxes.
 */
export function initAccountLinkingCompos(onSearch, onLink) {
    const targetEl = document.getElementById('targetAccountCombobox');
    const masterEl = document.getElementById('masterAccountCombobox');
    const linkBtn = document.getElementById('linkAccountsBtn');

    if (!targetEl || !masterEl || !linkBtn || !window.Combobox) return;

    const targetCombo = new window.Combobox(targetEl, {
        placeholder: 'Select target account...',
        onSearch: onSearch
    });

    const masterCombo = new window.Combobox(masterEl, {
        placeholder: 'Select master account...',
        onSearch: onSearch
    });

    linkBtn.onclick = () => {
        const target = targetCombo.getValue();
        const master = masterCombo.getValue();
        if (target && master) {
            onLink(target, master);
            targetCombo.clear();
            masterCombo.clear();
        }
    };

    return { targetCombo, masterCombo };
}
