import { escapeHTML } from '../utils';

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

export function renderProposals(
    proposals: any[],
    onAccept: (groupId: string, canonicalName: string) => void,
    onReject: (groupId: string) => void
): void {
    const container = document.getElementById('proposalsList');
    if (!container) return;

    if (!proposals || proposals.length === 0) {
        container.innerHTML = '<p class="u-text-dim u-text-xs">제안 없음. AI 분석 실행 버튼을 눌러 분석을 시작하세요.</p>';
        return;
    }

    container.innerHTML = proposals.map((p: any) => {
        const groupId = escapeHTML(p.group_id || '');
        const contacts: any[] = p.contacts || [];
        const confidence = Math.round((p.confidence || 0) * 100);
        const reason = escapeHTML(p.reason || '');
        const names = contacts.map((c: any) => c.display_name || c.canonical_id).filter(Boolean);

        const chips = names.map(n => `<span class="c-proposal-card__chip">${escapeHTML(n)}</span>`).join('');
        const options = names.map(n => `<option value="${escapeHTML(n)}">${escapeHTML(n)}</option>`).join('');

        return `
        <div class="c-proposal-card">
            <div class="c-proposal-card__names">${chips}</div>
            <div class="c-proposal-card__meta">
                <span class="c-proposal-card__confidence">신뢰도 ${confidence}%</span>
                <span class="c-proposal-card__reason">${reason}</span>
            </div>
            <div class="c-proposal-card__actions">
                <select class="c-input c-proposal-card__canonical" data-group="${groupId}">
                    ${options}
                </select>
                <button class="c-btn c-btn--primary accept-proposal-btn" data-group="${groupId}">수락</button>
                <button class="c-btn c-btn--ghost reject-proposal-btn" data-group="${groupId}">거절</button>
            </div>
        </div>`;
    }).join('');

    container.querySelectorAll('.accept-proposal-btn').forEach(btn => {
        const groupId = (btn as HTMLElement).dataset.group || '';
        btn.addEventListener('click', () => {
            const select = container.querySelector<HTMLSelectElement>(`.c-proposal-card__canonical[data-group="${groupId}"]`);
            const canonicalName = select?.value || '';
            onAccept(groupId, canonicalName);
        });
    });

    container.querySelectorAll('.reject-proposal-btn').forEach(btn => {
        const groupId = (btn as HTMLElement).dataset.group || '';
        btn.addEventListener('click', () => onReject(groupId));
    });
}
