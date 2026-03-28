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
 * Renders the list of tenant aliases in settings.
 */
export function renderTenantAliasList(aliases, onRemove) {
    const container = document.getElementById('normList');
    if (!container) return;

    const list = Array.isArray(aliases) ? aliases : (aliases?.aliases || aliases?.data || []);

    if (!list || list.length === 0) {
        container.innerHTML = '<p class="empty-list">No tenant aliases configured</p>';
        return;
    }

    container.innerHTML = list.map(alias => {
        const orig = alias.original_name || alias.original || alias;
        const prim = alias.primary_name || alias.primary || '';
        const displayStr = prim ? `${escapeHTML(orig)} → ${escapeHTML(prim)}` : escapeHTML(orig);

        return `
        <div class="c-settings__item">
            <span>${displayStr}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-tenant-alias-btn" data-alias="${escapeHTML(orig)}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.remove-tenant-alias-btn').forEach(btn => {
        btn.addEventListener('click', () => onRemove(btn.dataset.alias));
    });
}

/**
 * Renders contact mappings in settings.
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
        const rep = m.rep_name || m.repName || m.name || '';
        const aliases = m.aliases || m.aliasNames || m.alias || m.source || '';

        return `
        <div class="c-settings__item">
            <span>${escapeHTML(aliases)}</span>
            <span style="color: var(--text-dim);">→</span>
            <span style="font-weight: bold;">${escapeHTML(rep)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-mapping-btn" data-id="${escapeHTML(rep)}">&times;</button>
        </div>
        `;
    }).join('');

    container.querySelectorAll('.remove-mapping-btn').forEach(btn => {
        btn.addEventListener('click', () => onRemove(btn.dataset.id));
    });
}
