import { I18N_DATA } from '../locales.js';
import { DOM_IDS, STATUS_STATES, UI_TEXT } from '../constants.ts';
import { showToast } from './ui-effects.ts';

/**
 * @file status-renderer.ts
 * @description UI renderer for service connection statuses and QR code management.
 */

export type ServiceStatus = boolean | string;

/**
 * Common utility to update service status in the UI dashboard and settings.
 */
export function updateServiceStatusUI(service: string, status: ServiceStatus): void {
    let isConnected = status === true;
    if (typeof status === 'string') {
        const normalized = status.toLowerCase();
        isConnected = normalized === STATUS_STATES.CONNECTED.toLowerCase() || 
                      normalized === STATUS_STATES.AUTHENTICATED.toLowerCase();
    }

    const largeIcon = document.getElementById(DOM_IDS.STATUS_LARGE(service));
    const largeLabel = document.getElementById(DOM_IDS.STATUS_TEXT(service));

    if (largeIcon) {
        const activeClass = 'c-status-card--active';
        const inactiveClass = 'c-status-card--inactive';
        if (isConnected) {
            largeIcon.classList.add(activeClass);
            largeIcon.classList.remove(inactiveClass);
        } else {
            largeIcon.classList.add(inactiveClass);
            largeIcon.classList.remove(activeClass);
        }
    }
    if (largeLabel) {
        largeLabel.textContent = isConnected ? UI_TEXT.ON : UI_TEXT.OFF;
    }

    if (service === 'wa') {
        const qrSection = document.getElementById('waQRSection');
        const connectedSection = document.getElementById('waConnectedSection');
        if (qrSection) qrSection.classList.toggle('hidden', isConnected);
        if (connectedSection) connectedSection.classList.toggle('hidden', !isConnected);
    }

    if (service === 'gmail') {
        const connectedInfo = document.getElementById('gmailConnectedInfo');
        const disconnectedInfo = document.getElementById('gmailDisconnectedInfo');
        if (connectedInfo) connectedInfo.classList.toggle('hidden', !isConnected);
        if (disconnectedInfo) disconnectedInfo.classList.toggle('hidden', isConnected);
    }

    const settingsPill = document.getElementById(`${service}ConnectedStatus`);
    if (settingsPill) {
        settingsPill.classList.toggle('hidden', !isConnected);
    }
}

export function updateSlackStatus(status: ServiceStatus): void {
    updateServiceStatusUI('slack', status);
}

export function updateWhatsAppStatus(statusStr: ServiceStatus): void {
    updateServiceStatusUI('wa', statusStr);
}

export function updateGmailStatus(connected: boolean, email: string | undefined): void {
    updateServiceStatusUI('gmail', connected);
    const emailEl = document.getElementById('gmailEmail');
    if (emailEl) {
        emailEl.textContent = email || '';
        emailEl.classList.toggle('hidden', !connected);
    }
}

export function showWaModal(): void {
    const modal = document.getElementById('waModal');
    if (modal) {
        modal.classList.remove('hidden');
        modal.style.display = 'flex';
    }
}

export function showGmailModal(): void {
    const modal = document.getElementById('gmailModal');
    if (modal) {
        modal.classList.remove('hidden');
        modal.style.display = 'flex';
    }
}

export function bindGetQRBtn(onClick: (ev: MouseEvent) => void): void {
    document.getElementById('getQRBtn')?.addEventListener('click', onClick);
}

export function updateWhatsAppQR(status: 'generating' | 'show' | 'success' | 'error', data: string | null, lang?: string): void {
    const btn = document.getElementById('getQRBtn') as HTMLButtonElement | null;
    const img = document.getElementById('waQRImg') as HTMLImageElement | null;
    const placeholder = document.getElementById('qrPlaceholder');
    const i18n = (I18N_DATA as any)[lang || 'ko'];

    if (!btn || !img || !placeholder) return;

    if (status === 'generating') {
        btn.disabled = true;
        placeholder.textContent = i18n.generating || 'Generating...';
        placeholder.classList.remove('hidden');
        img.classList.add('hidden');
    } else if (status === 'show') {
        img.src = `data:image/png;base64,${data}`;
        img.classList.remove('hidden');
        placeholder.classList.add('hidden');
    } else if (status === 'success') {
        btn.disabled = false;
        img.classList.add('hidden');
        document.getElementById('qrTimerContainer')?.classList.add('hidden');
        placeholder.textContent = '✅ Connected!';
        placeholder.classList.remove('hidden');
        setTimeout(() => {
            const modal = document.getElementById('waModal');
            if (modal) {
                modal.classList.add('hidden');
                modal.style.display = 'none';
            }
        }, 2000);
        showToast(i18n.waConnected || 'WhatsApp connected!', 'success');
    } else if (status === 'error') {
        document.getElementById('qrTimerContainer')?.classList.add('hidden');
        placeholder.textContent = i18n.error || 'Error';
        showToast((i18n.qrError || 'Error: ') + (data || ''), 'error');
        btn.disabled = false;
    }
}

export function updateQRTimer(remaining: number, total: number): void {
    const container = document.getElementById('qrTimerContainer');
    const bar = document.getElementById('qrProgressBar') as HTMLElement | null;
    const text = document.getElementById('qrTimerText');
    if (!container || !bar || !text) return;

    container.classList.remove('hidden');
    const percentage = (remaining / total) * 100;
    bar.style.width = `${percentage}%`;
    text.textContent = `Next refresh in ${remaining}s`;
}

export function bindGmailStatus(onClick: (ev: MouseEvent) => void): void {
    document.getElementById('gmailStatusLarge')?.addEventListener('click', onClick);
}

export function bindWhatsAppStatus(onClick: (ev: MouseEvent) => void): void {
    document.getElementById('waStatusLarge')?.addEventListener('click', onClick);
}
