/**
 * @file telegram-modal-renderer.ts
 * @description 4-step Telegram auth modal (credentials → phone → OTP → optional 2FA) plus connected view.
 * Orchestrates DOM state transitions between the backend status enum and the visible step.
 */
import { api } from '../api';
import { TELEGRAM_STATUS } from '../constants';
import { showToast } from './ui-effects';
import { updateTelegramStatus, hideTelegramModal } from './status-renderer';
import { getErrorMessage } from '../utils';

type StepId = 'credentials' | 'phone' | 'code' | 'password' | 'connected';

const STEP_IDS: Record<StepId, string> = {
    credentials: 'telegramStepCredentials',
    phone: 'telegramStepPhone',
    code: 'telegramStepCode',
    password: 'telegramStepPassword',
    connected: 'telegramConnectedSection'
};

function showStep(step: StepId): void {
    (Object.keys(STEP_IDS) as StepId[]).forEach(k => {
        const el = document.getElementById(STEP_IDS[k]);
        if (el) el.classList.toggle('hidden', k !== step);
    });
    clearError();
}

function setError(msg: string): void {
    const errEl = document.getElementById('telegramModalError');
    if (errEl) {
        errEl.textContent = msg;
        errEl.classList.remove('hidden');
    }
}

function clearError(): void {
    const errEl = document.getElementById('telegramModalError');
    if (errEl) {
        errEl.textContent = '';
        errEl.classList.add('hidden');
    }
}

function setBusy(btnId: string, busy: boolean): void {
    const btn = document.getElementById(btnId) as HTMLButtonElement | null;
    if (btn) btn.disabled = busy;
}

/**
 * Why: Reflect the server-side status string onto the visible step when the modal opens.
 * When the user has no stored App ID/Hash yet, show the credentials step first so the
 * phone submission doesn't fail with a cryptic 500.
 */
export function syncTelegramModalToStatus(status: string, hasCredentials: boolean = true): void {
    if (status === TELEGRAM_STATUS.CONNECTED) {
        showStep('connected');
        return;
    }
    if (!hasCredentials) {
        showStep('credentials');
        return;
    }
    if (status === TELEGRAM_STATUS.PENDING_PASSWORD) {
        showStep('password');
    } else if (status === TELEGRAM_STATUS.PENDING_CODE) {
        showStep('code');
    } else {
        showStep('phone');
    }
}

async function handleCredentialsSubmit(): Promise<void> {
    const idInput = document.getElementById('telegramAppIdInput') as HTMLInputElement | null;
    const hashInput = document.getElementById('telegramAppHashInput') as HTMLInputElement | null;
    const appId = parseInt((idInput?.value || '').trim(), 10);
    const appHash = (hashInput?.value || '').trim();
    if (!Number.isInteger(appId) || appId <= 0) {
        setError('App ID must be a positive integer');
        return;
    }
    if (!appHash) {
        setError('App Hash is required');
        return;
    }
    setBusy('telegramSaveCredsBtn', true);
    clearError();
    try {
        await api.saveTelegramCredentials(appId, appHash);
        showToast('Telegram credentials saved', 'success');
        showStep('phone');
    } catch (e: unknown) {
        setError(getErrorMessage(e) || 'Failed to save credentials');
    } finally {
        setBusy('telegramSaveCredsBtn', false);
    }
}

async function handlePhoneSubmit(onConnected: () => void): Promise<void> {
    const input = document.getElementById('telegramPhoneInput') as HTMLInputElement | null;
    const phone = (input?.value || '').trim();
    if (!phone) {
        setError('Phone number is required');
        return;
    }
    setBusy('telegramSendCodeBtn', true);
    clearError();
    try {
        await api.startTelegramAuth(phone);
        showStep('code');
    } catch (e: unknown) {
        setError(getErrorMessage(e) || 'Failed to send code');
    } finally {
        setBusy('telegramSendCodeBtn', false);
        void onConnected;
    }
}

async function handleCodeSubmit(onConnected: () => void): Promise<void> {
    const input = document.getElementById('telegramCodeInput') as HTMLInputElement | null;
    const code = (input?.value || '').trim();
    if (!code) {
        setError('Code is required');
        return;
    }
    setBusy('telegramConfirmCodeBtn', true);
    clearError();
    try {
        const res = await api.confirmTelegramCode(code);
        if (res.status === TELEGRAM_STATUS.PENDING_PASSWORD) {
            showStep('password');
            return;
        }
        updateTelegramStatus(TELEGRAM_STATUS.CONNECTED);
        showToast('Telegram connected', 'success');
        onConnected();
    } catch (e: unknown) {
        setError(getErrorMessage(e) || 'Invalid code');
    } finally {
        setBusy('telegramConfirmCodeBtn', false);
    }
}

async function handlePasswordSubmit(onConnected: () => void): Promise<void> {
    const input = document.getElementById('telegramPasswordInput') as HTMLInputElement | null;
    const password = input?.value || '';
    if (!password) {
        setError('Password is required');
        return;
    }
    setBusy('telegramConfirmPasswordBtn', true);
    clearError();
    try {
        await api.confirmTelegramPassword(password);
        updateTelegramStatus(TELEGRAM_STATUS.CONNECTED);
        showToast('Telegram connected', 'success');
        onConnected();
    } catch (e: unknown) {
        setError(getErrorMessage(e) || 'Invalid password');
    } finally {
        setBusy('telegramConfirmPasswordBtn', false);
    }
}

async function handleLogout(onLoggedOut: () => void): Promise<void> {
    if (!confirm('Disconnect Telegram?')) return;
    setBusy('telegramDisconnectBtn', true);
    try {
        await api.logoutTelegram();
        resetTelegramModalInputs();
        updateTelegramStatus(TELEGRAM_STATUS.DISCONNECTED);
        showStep('phone');
        hideTelegramModal();
        showToast('Telegram disconnected', 'success');
        onLoggedOut();
    } catch (e: unknown) {
        setError(getErrorMessage(e) || 'Logout failed');
    } finally {
        setBusy('telegramDisconnectBtn', false);
    }
}

export function resetTelegramModalInputs(): void {
    ['telegramPhoneInput', 'telegramCodeInput', 'telegramPasswordInput'].forEach(id => {
        const el = document.getElementById(id) as HTMLInputElement | null;
        if (el) el.value = '';
    });
    clearError();
}

/**
 * Exposes "change credentials" path from the phone step so users can re-enter App ID/Hash
 * without needing a full logout.
 */
export function showTelegramCredentialsStep(): void {
    showStep('credentials');
}

/**
 * Wires up all click handlers inside the Telegram modal. Idempotent: safe to call multiple times.
 */
export function bindTelegramModal(callbacks: { onConnected: () => void; onLoggedOut: () => void }): void {
    const root = document.getElementById('telegramModal');
    if (!root || root.dataset.bound === '1') return;
    root.dataset.bound = '1';

    document.getElementById('telegramSaveCredsBtn')?.addEventListener('click', handleCredentialsSubmit);
    document.getElementById('telegramSendCodeBtn')?.addEventListener('click', () => handlePhoneSubmit(callbacks.onConnected));
    document.getElementById('telegramConfirmCodeBtn')?.addEventListener('click', () => handleCodeSubmit(callbacks.onConnected));
    document.getElementById('telegramConfirmPasswordBtn')?.addEventListener('click', () => handlePasswordSubmit(callbacks.onConnected));
    document.getElementById('telegramDisconnectBtn')?.addEventListener('click', () => handleLogout(callbacks.onLoggedOut));
    document.getElementById('telegramChangeCredsBtn')?.addEventListener('click', () => showStep('credentials'));

    document.getElementById('closeTelegramModalBtn')?.addEventListener('click', hideTelegramModal);

    const enterSubmit = (inputId: string, btnId: string) => {
        const input = document.getElementById(inputId) as HTMLInputElement | null;
        input?.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                (document.getElementById(btnId) as HTMLButtonElement | null)?.click();
            }
        });
    };
    enterSubmit('telegramAppHashInput', 'telegramSaveCredsBtn');
    enterSubmit('telegramPhoneInput', 'telegramSendCodeBtn');
    enterSubmit('telegramCodeInput', 'telegramConfirmCodeBtn');
    enterSubmit('telegramPasswordInput', 'telegramConfirmPasswordBtn');
}
