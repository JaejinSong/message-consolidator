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
