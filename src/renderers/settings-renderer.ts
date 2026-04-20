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

function renderSettingsList<T>(
    containerId: string,
    items: T[],
    emptyHTML: string,
    renderItem: (item: T) => string,
    btnSelector: string,
    onAction: (key: string) => void,
    dataKey = 'id'
): void {
    const container = document.getElementById(containerId);
    if (!container) return;

    if (!items || items.length === 0) {
        container.innerHTML = emptyHTML;
        return;
    }

    container.innerHTML = items.map(renderItem).join('');

    container.querySelectorAll(btnSelector).forEach(btn => {
        const key = (btn as HTMLElement).dataset[dataKey];
        if (key) btn.addEventListener('click', () => onAction(key));
    });
}

function normalizeList(data: any, fallbackKey: string): any[] {
    if (Array.isArray(data)) return data;
    return data?.[fallbackKey] || data?.data || [];
}

export function renderAliasList(aliases: any, onRemove: (alias: string) => void): void {
    const list = normalizeList(aliases, 'aliases');
    renderSettingsList(
        'aliasList', list,
        '<p class="empty-list">No aliases configured</p>',
        (alias: string) => `
        <div class="c-settings__item">
            <span>${escapeHTML(alias)}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-alias-btn" data-alias="${escapeHTML(alias)}">&times;</button>
        </div>`,
        '.remove-alias-btn', onRemove, 'alias'
    );
}

export function renderTenantAliasList(aliases: any, onRemove: (id: string) => void): void {
    const list = normalizeList(aliases, 'aliases');
    renderSettingsList(
        'normList', list,
        '<p class="empty-list">No tenant aliases configured</p>',
        (item: any) => {
            if (!item || typeof item !== 'object') return '';
            const cid = item.canonical_id || "";
            const dname = item.display_name || "";
            const displayStr = dname ? `${escapeHTML(cid)} &rarr; ${escapeHTML(dname)}` : escapeHTML(cid);
            return `
        <div class="c-settings__item">
            <span>${displayStr}</span>
            <button class="c-btn c-btn--ghost c-btn--icon remove-tenant-alias-btn" data-id="${Number(item.id || 0)}">&times;</button>
        </div>`;
        },
        '.remove-tenant-alias-btn', onRemove
    );
}

export function renderContactMappings(mappings: any, onRemove: (id: string) => void): void {
    const list = normalizeList(mappings, 'mappings');
    renderSettingsList(
        'contactList', list,
        '<p class="empty-list">No contact mappings</p>',
        (m: any) => {
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
        </div>`;
        },
        '.remove-mapping-btn', onRemove
    );
}

export function renderLinkedAccounts(links: any[], onUnlink: (id: string) => void): void {
    renderSettingsList(
        'linkedAccountsList', links || [],
        '<p class="u-text-dim u-text-xs">No linked accounts found.</p>',
        (link: any) => {
            if (!link || !link.target || !link.master) return '';
            const targetLabel = escapeHTML(link.target.display_name || link.target.canonical_id);
            const masterLabel = escapeHTML(link.master.display_name || link.master.canonical_id);
            return `
        <div class="c-settings__item">
            <span class="u-text-accent">${targetLabel}</span>
            <span class="u-mx-2 u-text-dim">→</span>
            <span class="u-font-bold">${masterLabel}</span>
            <button class="c-btn c-btn--ghost c-btn--icon u-ml-2 unlink-btn" data-id="${Number(link.target_id)}">&times;</button>
        </div>`;
        },
        '.unlink-btn', onUnlink
    );
}

export function initAccountLinkingCompos(
    searchFn: (q: string) => Promise<AccountItem[]>,
    onLink: (targetId: number, masterId: number) => void
): SettingsCompos | undefined {
    const targetEl = document.getElementById('targetAccountCombobox');
    const masterEl = document.getElementById('masterAccountCombobox');
    const linkBtn = document.getElementById('linkAccountsBtn');

    if (!targetEl || !masterEl || !linkBtn) return undefined;

    const targetCombo = new Combobox(targetEl, { placeholder: 'Select target account...', searchFn });
    const masterCombo = new Combobox(masterEl, { placeholder: 'Select master account...', searchFn });

    linkBtn.onclick = () => {
        const target = targetCombo.getValue();
        const master = masterCombo.getValue();
        if (target && master) {
            onLink(Number(target.id), Number(master.id));
            targetCombo.clear();
            masterCombo.clear();
        }
    };

    return { targetCombo, masterCombo };
}
