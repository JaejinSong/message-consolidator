import { api } from '../api';
import { AdminSetting, AdminUser } from '../types';
import { escapeHTML, getErrorMessage } from '../utils';

const CATEGORY_LABELS: Record<string, string> = {
    auth: '인증/세션',
    ai: 'AI / Gemini',
    channels: '채널 통합',
    db: '데이터베이스',
    ops: '운영',
};

let cachedSettings: AdminSetting[] = [];
let cachedAdmins: AdminUser[] = [];
let initialized = false;

export async function renderAdminPanel(): Promise<void> {
    bindOnce();
    await Promise.all([loadAndRenderSettings(), loadAndRenderAdmins()]);
}

function bindOnce(): void {
    if (initialized) return;
    initialized = true;

    document.getElementById('adminSettingsList')?.addEventListener('submit', onSettingSubmit);
    document.getElementById('adminAddAdminBtn')?.addEventListener('click', onAddAdmin);
    document.getElementById('adminAdminsList')?.addEventListener('click', onAdminsListClick);
    document.getElementById('adminAddAdminInput')?.addEventListener('keydown', (event) => {
        if ((event as KeyboardEvent).key === 'Enter') {
            event.preventDefault();
            onAddAdmin();
        }
    });
}

async function loadAndRenderSettings(): Promise<void> {
    try {
        cachedSettings = await api.fetchAdminSettings();
        renderSettings(cachedSettings);
    } catch (err: unknown) {
        const list = document.getElementById('adminSettingsList');
        if (list) list.innerHTML = `<p class="u-text-dim">설정을 불러오지 못했습니다: ${escapeHTML(getErrorMessage(err))}</p>`;
    }
}

function renderSettings(settings: AdminSetting[]): void {
    const container = document.getElementById('adminSettingsList');
    if (!container) return;
    if (settings.length === 0) {
        container.innerHTML = '<p class="u-text-dim">등록된 설정이 없습니다.</p>';
        return;
    }

    const groups = groupByCategory(settings);
    const html = Object.keys(groups).map((cat) => {
        const items = groups[cat].map(renderSettingItem).join('');
        return `
            <div class="c-admin-settings__group">
                <h5 class="c-admin-settings__group-title">${escapeHTML(CATEGORY_LABELS[cat] || cat)}</h5>
                <div class="c-admin-settings__items">${items}</div>
            </div>
        `;
    }).join('');
    container.innerHTML = html;
}

function groupByCategory(settings: AdminSetting[]): Record<string, AdminSetting[]> {
    const out: Record<string, AdminSetting[]> = {};
    for (const s of settings) {
        if (!out[s.category]) out[s.category] = [];
        out[s.category].push(s);
    }
    return out;
}

function renderSettingItem(setting: AdminSetting): string {
    const inputType = setting.secret ? 'password' : (setting.type === 'int' ? 'number' : 'text');
    const placeholder = setting.secret && setting.has_value ? '•••••••• (저장됨)' : '';
    const valueAttr = setting.secret ? '' : `value="${escapeHTML(setting.value)}"`;
    const restartBadge = setting.restart_required
        ? `<span class="c-badge c-badge--warn" data-i18n="adminRestartRequired">재시작 필요</span>`
        : `<span class="c-badge c-badge--ok" data-i18n="adminHotReload">즉시 적용</span>`;
    const updatedHint = setting.updated_by
        ? `<span class="c-admin-settings__hint">${escapeHTML(setting.updated_by)} · ${escapeHTML(setting.updated_at || '')}</span>`
        : `<span class="c-admin-settings__hint">.env 기본값 사용 중</span>`;
    const enumOptions = setting.enum_values
        ? setting.enum_values.map(v => `<option value="${escapeHTML(v)}" ${v === setting.value ? 'selected' : ''}>${escapeHTML(v)}</option>`).join('')
        : '';
    const inputHTML = setting.type === 'enum'
        ? `<select name="value" class="c-input">${enumOptions}</select>`
        : `<input name="value" type="${inputType}" class="c-input" placeholder="${escapeHTML(placeholder)}" ${valueAttr} autocomplete="off" />`;

    return `
        <form class="c-admin-settings__row" data-key="${escapeHTML(setting.key)}">
            <div class="c-admin-settings__meta">
                <label class="c-admin-settings__label">${escapeHTML(setting.label)} ${restartBadge}</label>
                <code class="c-admin-settings__key">${escapeHTML(setting.key)}</code>
                ${updatedHint}
            </div>
            <div class="c-admin-settings__control">
                ${inputHTML}
                <button type="submit" class="c-btn c-btn--primary">저장</button>
                <button type="button" class="c-btn c-btn--ghost" data-action="reset">초기화</button>
            </div>
        </form>
    `;
}

async function onSettingSubmit(event: Event): Promise<void> {
    const target = event.target as HTMLElement;
    if (!(target instanceof HTMLFormElement)) return;
    event.preventDefault();
    const key = target.dataset.key;
    if (!key) return;

    const submitter = (event as SubmitEvent).submitter as HTMLButtonElement | null;
    const isReset = submitter?.dataset.action === 'reset';
    const input = target.querySelector<HTMLInputElement | HTMLSelectElement>('[name="value"]');
    const value = isReset ? '' : (input?.value || '');

    try {
        const result = await api.updateAdminSetting(key, value);
        const msg = isReset ? '.env 기본값으로 초기화되었습니다' : (result.applied ? '즉시 적용되었습니다' : '재시작 후 반영됩니다');
        notify(msg);
        await loadAndRenderSettings();
    } catch (err: unknown) {
        notify(`저장 실패: ${getErrorMessage(err)}`, true);
    }
}

async function loadAndRenderAdmins(): Promise<void> {
    try {
        cachedAdmins = await api.fetchAdmins();
        renderAdmins(cachedAdmins);
    } catch (err: unknown) {
        const list = document.getElementById('adminAdminsList');
        if (list) list.innerHTML = `<p class="u-text-dim">관리자 목록을 불러오지 못했습니다: ${escapeHTML(getErrorMessage(err))}</p>`;
    }
}

function renderAdmins(admins: AdminUser[]): void {
    const list = document.getElementById('adminAdminsList');
    if (!list) return;
    if (admins.length === 0) {
        list.innerHTML = '<p class="u-text-dim">관리자가 없습니다.</p>';
        return;
    }
    list.innerHTML = admins.map(a => `
        <div class="c-admin-admins__row">
            <span class="c-admin-admins__email">${escapeHTML(a.email)}</span>
            <span class="c-admin-admins__name">${escapeHTML(a.name || '')}</span>
            ${a.is_super
                ? '<span class="c-badge c-badge--info">Super Admin</span>'
                : `<button type="button" class="c-btn c-btn--ghost" data-action="revoke" data-email="${escapeHTML(a.email)}">회수</button>`
            }
        </div>
    `).join('');
}

async function onAddAdmin(): Promise<void> {
    const input = document.getElementById('adminAddAdminInput') as HTMLInputElement | null;
    const email = (input?.value || '').trim();
    if (!email) {
        notify('이메일을 입력해 주세요', true);
        return;
    }
    try {
        await api.addAdmin(email);
        if (input) input.value = '';
        notify(`${email} 에게 관리자 권한을 위임했습니다`);
        await loadAndRenderAdmins();
    } catch (err: unknown) {
        notify(`위임 실패: ${getErrorMessage(err)}`, true);
    }
}

async function onAdminsListClick(event: Event): Promise<void> {
    const target = event.target as HTMLElement;
    const btn = target.closest<HTMLButtonElement>('[data-action="revoke"]');
    if (!btn) return;
    const email = btn.dataset.email || '';
    if (!email) return;
    if (!confirm(`${email} 의 관리자 권한을 회수하시겠습니까?`)) return;
    try {
        await api.removeAdmin(email);
        notify(`${email} 의 관리자 권한을 회수했습니다`);
        await loadAndRenderAdmins();
    } catch (err: unknown) {
        notify(`회수 실패: ${getErrorMessage(err)}`, true);
    }
}

function notify(message: string, isError = false): void {
    // Why: lightweight inline toast — the project does not export a shared notification API
    // from a single module, and pulling one in would balloon scope. Falls back to alert if console-only.
    const banner = document.createElement('div');
    banner.className = `c-admin-toast ${isError ? 'c-admin-toast--error' : ''}`;
    banner.textContent = message;
    document.body.appendChild(banner);
    setTimeout(() => banner.remove(), 3500);
}
