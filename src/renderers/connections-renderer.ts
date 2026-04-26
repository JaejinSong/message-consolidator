/**
 * @file connections-renderer.ts
 * @description Settings → Connections 탭의 4개 채널 카드 렌더링과 액션 핸들러.
 * 기존 wa/gmail/telegram modal 자산을 sub-modal로 호출만 하고, 카드 자체는
 * status polling 결과(api.fetchAllStatuses)로부터 갱신된다.
 */

import { api } from '../api';
import { authService } from '../services/authService';
import { state } from '../state';
import { t } from '../i18n';
import { escapeHTML } from '../utils';
import { showToast } from './ui-effects';
import { showWaModal, showTelegramModal } from './status-renderer';
import { showTelegramCredentialsStep, syncTelegramModalToStatus } from './telegram-modal-renderer';

export interface ConnectionsState {
    gmail: { connected: boolean; email?: string };
    whatsapp: { connected: boolean; deviceName?: string };
    telegram: { status: string; hasCredentials?: boolean; phoneMasked?: string; appIdMasked?: string };
    slack: { connected: boolean; slackId?: string };
}

type ChannelKey = 'gmail' | 'whatsapp' | 'telegram' | 'slack';

const CARD_IDS: Record<ChannelKey, string> = {
    gmail: 'connCard-gmail',
    whatsapp: 'connCard-whatsapp',
    telegram: 'connCard-telegram',
    slack: 'connCard-slack',
};

let cachedState: ConnectionsState | null = null;
let initialized = false;

export function setupConnectionsTab(): void {
    if (initialized) return;
    const root = document.getElementById('connectionsList');
    if (!root) return;

    root.innerHTML = (['gmail', 'whatsapp', 'telegram', 'slack'] as ChannelKey[])
        .map(channel => buildCardSkeleton(channel))
        .join('');

    bindCardActions();
    initialized = true;

    if (cachedState) {
        renderConnections(cachedState);
    }
}

/**
 * Renders the four cards using the latest status snapshot. Callers should pass
 * the same data they already pulled for the dashboard's channel icons — no extra fetch.
 */
export function renderConnections(snapshot: ConnectionsState): void {
    cachedState = snapshot;
    if (!initialized) return;

    const lang = state.currentLang || 'en';
    renderGmail(snapshot.gmail, lang);
    renderWhatsApp(snapshot.whatsapp, lang);
    renderTelegram(snapshot.telegram, lang);
    renderSlack(snapshot.slack, lang);
}

/** Re-renders cards using the most recent snapshot — call after the UI language changes. */
export function rerenderConnections(): void {
    if (cachedState) renderConnections(cachedState);
}

function buildCardSkeleton(channel: ChannelKey): string {
    return `
        <article class="c-connection-card" id="${CARD_IDS[channel]}" data-channel="${channel}">
            <header class="c-connection-card__header">
                <span class="c-connection-card__icon">${channelIcon(channel)}</span>
                <span class="c-connection-card__name">${channelName(channel)}</span>
                <span class="c-connection-card__badge" data-role="badge"></span>
            </header>
            <div class="c-connection-card__meta" data-role="meta"></div>
            <div class="c-connection-card__notice hidden" data-role="notice"></div>
            <div class="c-connection-card__actions" data-role="actions"></div>
        </article>
    `;
}

function channelName(channel: ChannelKey): string {
    switch (channel) {
        case 'gmail': return 'Gmail';
        case 'whatsapp': return 'WhatsApp';
        case 'telegram': return 'Telegram';
        case 'slack': return 'Slack';
    }
}

// Inline SVG to avoid pulling icon assets — sized via CSS (currentColor + 1.5rem container).
function channelIcon(channel: ChannelKey): string {
    switch (channel) {
        case 'gmail':
            return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M22 7 12 13 2 7"/></svg>';
        case 'whatsapp':
            return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>';
        case 'telegram':
            return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/></svg>';
        case 'slack':
            return '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="6" height="6" rx="1.5"/><rect x="15" y="3" width="6" height="6" rx="1.5"/><rect x="3" y="15" width="6" height="6" rx="1.5"/><rect x="15" y="15" width="6" height="6" rx="1.5"/></svg>';
    }
}

function setBadge(card: HTMLElement, connected: boolean, lang: string): void {
    const el = card.querySelector<HTMLElement>('[data-role=badge]');
    if (!el) return;
    el.classList.remove('c-connection-card__badge--connected', 'c-connection-card__badge--disconnected');
    el.classList.add(connected ? 'c-connection-card__badge--connected' : 'c-connection-card__badge--disconnected');
    el.textContent = connected ? t('connStatusConnected', lang) : t('connStatusDisconnected', lang);
}

function setMeta(card: HTMLElement, rows: { key: string; value: string }[]): void {
    const el = card.querySelector<HTMLElement>('[data-role=meta]');
    if (!el) return;
    if (rows.length === 0) {
        el.innerHTML = '';
        return;
    }
    el.innerHTML = rows.map(r => `
        <span class="c-connection-card__meta-key">${escapeHTML(r.key)}</span>
        <span class="c-connection-card__meta-value">${escapeHTML(r.value)}</span>
    `).join('');
}

function setNotice(card: HTMLElement, text: string | null): void {
    const el = card.querySelector<HTMLElement>('[data-role=notice]');
    if (!el) return;
    if (!text) {
        el.classList.add('hidden');
        el.textContent = '';
        return;
    }
    el.classList.remove('hidden');
    el.textContent = text;
}

function setActions(card: HTMLElement, actions: { id: string; labelKey: string; variant: 'primary' | 'ghost' }[], lang: string): void {
    const el = card.querySelector<HTMLElement>('[data-role=actions]');
    if (!el) return;
    el.innerHTML = actions.map(a => `
        <button type="button" class="c-btn c-btn--${a.variant}" data-action="${a.id}">${t(a.labelKey, lang)}</button>
    `).join('');
}

function setCardModifier(card: HTMLElement, connected: boolean): void {
    card.classList.toggle('c-connection-card--disconnected', !connected);
}

function renderGmail(s: ConnectionsState['gmail'], lang: string): void {
    const card = document.getElementById(CARD_IDS.gmail);
    if (!card) return;
    setBadge(card, s.connected, lang);
    setCardModifier(card, s.connected);
    setNotice(card, null);

    if (s.connected) {
        setMeta(card, [{ key: t('connEmailLabel', lang), value: s.email || t('connEmptyValue', lang) }]);
        setActions(card, [
            { id: 'gmail-reauth', labelKey: 'connReauthBtn', variant: 'ghost' },
            { id: 'gmail-disconnect', labelKey: 'connDisconnectBtn', variant: 'ghost' },
        ], lang);
    } else {
        setMeta(card, []);
        setActions(card, [
            { id: 'gmail-connect', labelKey: 'connConnectBtn', variant: 'primary' },
        ], lang);
    }
}

function renderWhatsApp(s: ConnectionsState['whatsapp'], lang: string): void {
    const card = document.getElementById(CARD_IDS.whatsapp);
    if (!card) return;
    setBadge(card, s.connected, lang);
    setCardModifier(card, s.connected);
    setNotice(card, null);

    if (s.connected) {
        setMeta(card, [
            { key: t('connDeviceLabel', lang), value: s.deviceName || t('connEmptyValue', lang) },
        ]);
        setActions(card, [
            { id: 'wa-rescan', labelKey: 'connRescanBtn', variant: 'ghost' },
            { id: 'wa-logout', labelKey: 'connLogoutBtn', variant: 'ghost' },
        ], lang);
    } else {
        setMeta(card, []);
        setActions(card, [
            { id: 'wa-connect', labelKey: 'connConnectBtn', variant: 'primary' },
        ], lang);
    }
}

function renderTelegram(s: ConnectionsState['telegram'], lang: string): void {
    const card = document.getElementById(CARD_IDS.telegram);
    if (!card) return;
    const connected = s.status === 'connected';
    setBadge(card, connected, lang);
    setCardModifier(card, connected);
    setNotice(card, null);

    if (connected) {
        const rows: { key: string; value: string }[] = [];
        if (s.phoneMasked) rows.push({ key: t('connPhoneLabel', lang), value: s.phoneMasked });
        if (s.appIdMasked) rows.push({ key: t('connAppIdLabel', lang), value: s.appIdMasked });
        setMeta(card, rows);
        setActions(card, [
            { id: 'tg-change-creds', labelKey: 'connChangeCredsBtn', variant: 'ghost' },
            { id: 'tg-reauth', labelKey: 'connReauthBtn', variant: 'ghost' },
            { id: 'tg-logout', labelKey: 'connLogoutBtn', variant: 'ghost' },
        ], lang);
    } else {
        setMeta(card, []);
        setActions(card, [
            { id: 'tg-connect', labelKey: 'connConnectBtn', variant: 'primary' },
        ], lang);
    }
}

function renderSlack(s: ConnectionsState['slack'], lang: string): void {
    const card = document.getElementById(CARD_IDS.slack);
    if (!card) return;
    setBadge(card, s.connected, lang);
    setCardModifier(card, s.connected);

    if (s.slackId) {
        setMeta(card, [{ key: t('connSlackIdLabel', lang), value: s.slackId }]);
        setNotice(card, t('connSlackReadOnlyNotice', lang));
    } else {
        setMeta(card, []);
        setNotice(card, s.connected ? t('connNoMappingNotice', lang) : t('connSlackReadOnlyNotice', lang));
    }
    setActions(card, [], lang);
}

function bindCardActions(): void {
    const root = document.getElementById('connectionsList');
    if (!root) return;
    root.addEventListener('click', (ev) => {
        const target = (ev.target as HTMLElement | null)?.closest('[data-action]');
        if (!target) return;
        const action = target.getAttribute('data-action');
        if (!action) return;
        handleAction(action);
    });
}

async function handleAction(action: string): Promise<void> {
    switch (action) {
        case 'gmail-connect':
        case 'gmail-reauth':
            authService.connectGmail();
            return;
        case 'gmail-disconnect': {
            try {
                await api.disconnectGmail();
                showToast(t('gmailStatusConnectedToast', state.currentLang || 'en'), 'success');
            } catch (e) {
                showToast(String(e), 'error');
            }
            return;
        }
        case 'wa-connect':
        case 'wa-rescan':
            showWaModal();
            return;
        case 'wa-logout': {
            try { await api.logoutWhatsApp(); } catch (e) { showToast(String(e), 'error'); }
            return;
        }
        case 'tg-connect':
        case 'tg-reauth':
            showTelegramModal();
            // Sync to current status — if disconnected, modal opens at phone step.
            try {
                const s = await api.fetchTelegramStatus();
                syncTelegramModalToStatus(s.status, s.has_credentials ?? false);
            } catch { /* modal still usable with default phone step */ }
            return;
        case 'tg-change-creds':
            showTelegramModal();
            showTelegramCredentialsStep();
            return;
        case 'tg-logout': {
            try { await api.logoutTelegram(); } catch (e) { showToast(String(e), 'error'); }
            return;
        }
    }
}
