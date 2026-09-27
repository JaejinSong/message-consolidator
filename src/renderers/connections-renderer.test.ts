// @vitest-environment happy-dom
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { setupConnectionsTab, renderConnections, handleSlackAuthRedirect, ConnectionsState } from './connections-renderer';
import { state } from '../state';
import { I18N_DATA } from '../locales';

function baseSnapshot(slack: ConnectionsState['slack']): ConnectionsState {
    return {
        gmail: { connected: false },
        whatsapp: { connected: false },
        telegram: { status: 'disconnected' },
        slack,
        line: { connected: false },
    };
}

describe('Slack connection card', () => {
    beforeAll(() => {
        document.body.innerHTML = '<div id="connectionsList"></div>';
        setupConnectionsTab();
    });

    beforeEach(() => {
        state.currentLang = 'en';
    });

    it('shows the connect action and notice when user_token is false', () => {
        renderConnections(baseSnapshot({ connected: true }));
        const card = document.getElementById('connCard-slack')!;
        const connectBtn = card.querySelector('[data-action="slack-connect"]');
        expect(connectBtn?.textContent).toBe(I18N_DATA['en'].connSlackConnectBtn);
        expect(card.querySelector('[data-role=notice]')?.textContent).toBe(I18N_DATA['en'].connSlackConnectNotice);
        expect(card.querySelector('[data-action="slack-reauth"]')).toBeNull();
        expect(card.querySelector('[data-action="slack-disconnect"]')).toBeNull();
    });

    it('shows the Slack account meta and reauth/disconnect actions when user_token is true', () => {
        renderConnections(baseSnapshot({ connected: true, userToken: true, userTokenSlackId: 'U123' }));
        const card = document.getElementById('connCard-slack')!;
        const meta = card.querySelector('[data-role=meta]')!;
        expect(meta.textContent).toContain(I18N_DATA['en'].connSlackAccountLabel);
        expect(meta.textContent).toContain('U123');
        expect(card.querySelector('[data-action="slack-reauth"]')?.textContent).toBe(I18N_DATA['en'].connReauthBtn);
        expect(card.querySelector('[data-action="slack-disconnect"]')?.textContent).toBe(I18N_DATA['en'].connDisconnectBtn);
        expect(card.querySelector('[data-action="slack-connect"]')).toBeNull();
    });

    it('falls back to the empty value placeholder when user_token_slack_id is blank', () => {
        renderConnections(baseSnapshot({ connected: true, userToken: true, userTokenSlackId: '' }));
        const card = document.getElementById('connCard-slack')!;
        expect(card.querySelector('[data-role=meta]')?.textContent).toContain(I18N_DATA['en'].connEmptyValue);
    });
});

describe('handleSlackAuthRedirect', () => {
    beforeEach(() => {
        state.currentLang = 'en';
        document.querySelectorAll('.toast-popup').forEach(el => el.remove());
        window.history.replaceState({}, '', '/');
    });

    it('does nothing when there is no slack query param', () => {
        window.history.replaceState({}, '', '/?foo=bar');
        handleSlackAuthRedirect();
        expect(document.querySelector('.toast-popup')).toBeNull();
        expect(window.location.search).toBe('?foo=bar');
    });

    it('shows a success toast for slack=connected and strips the param', () => {
        window.history.replaceState({}, '', '/?slack=connected');
        handleSlackAuthRedirect();
        const toast = document.querySelector('.toast-popup');
        expect(toast?.className).toContain('toast-success');
        expect(toast?.textContent).toContain(I18N_DATA['en'].slackOAuthConnectedToast);
        expect(window.location.search).toBe('');
    });

    it('shows an info toast for slack=denied', () => {
        window.history.replaceState({}, '', '/?slack=denied');
        handleSlackAuthRedirect();
        const toast = document.querySelector('.toast-popup');
        expect(toast?.className).toContain('toast-info');
        expect(toast?.textContent).toContain(I18N_DATA['en'].slackOAuthDeniedToast);
    });

    it('shows an error toast for slack=mismatch', () => {
        window.history.replaceState({}, '', '/?slack=mismatch');
        handleSlackAuthRedirect();
        const toast = document.querySelector('.toast-popup');
        expect(toast?.className).toContain('toast-error');
        expect(toast?.textContent).toContain(I18N_DATA['en'].slackOAuthMismatchToast);
    });

    it('preserves other query params after stripping slack', () => {
        window.history.replaceState({}, '', '/?tab=connections&slack=connected');
        handleSlackAuthRedirect();
        expect(window.location.search).toBe('?tab=connections');
    });
});
