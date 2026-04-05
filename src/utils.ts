import { I18N_DATA } from './locales';

/**
 * @file utils.ts
 * @description Shared utility functions and services with TypeScript.
 */

/**
 * Higher-order function for common async error handling.
 */
export const safeAsync = <T extends any[], R>(
    fn: (...args: T) => Promise<R>,
    options: {
        triggerAuthOverlay?: boolean;
        onError?: (e: any) => void;
    } = {}
) => {
    return async function(this: any, ...args: T): Promise<R | undefined> {
        const { triggerAuthOverlay = false, onError } = options;
        try {
            return await fn.apply(this, args);
        } catch (e: any) {
            console.error('[Async Error]', e);
            if (e.isAuthError && triggerAuthOverlay) {
                if (!hasSessionHint()) {
                    console.warn('[safeAsync] AuthError and no session hint. Triggering login overlay.');
                    const overlay = document.getElementById('loginOverlay');
                    if (overlay) {
                        overlay.classList.remove('hidden');
                        overlay.style.display = 'flex';
                    }
                }
            }
            if (onError) onError(e);
            throw e;
        }
    };
};

/**
 * Checks for session hint in cookies.
 */
export const hasSessionHint = (): boolean => {
    return document.cookie.split(';').some(item => item.trim().startsWith('session_active=true'));
};

/**
 * TimeService provides a centralized system for all date/time operations.
 */
export const TimeService = {
    getLocalDateString(date: Date = new Date()): string {
        return date.toLocaleDateString('en-CA'); // YYYY-MM-DD
    },

    getDiffInDays(date1: Date, date2: Date): number {
        return Math.floor(Math.abs(date1.getTime() - date2.getTime()) / (1000 * 60 * 60 * 24));
    },

    formatDisplayTime(isoStr: string, lang: string = 'en'): string {
        return formatDisplayTime(isoStr, lang);
    }
};

/**
 * Formats a date string for display, using relative time for recent events.
 */
export function formatDisplayTime(isoStr: string, lang: string = 'en'): string {
    if (!isoStr) return '-';

    let dateStr: string = isoStr;
    // Legacy handling
    if (typeof dateStr === 'string') {
        dateStr = dateStr.replace(' KST', ' +0900').replace(' JKT', ' +0700').replace(' ICT', ' +0700');
        if (dateStr.match(/^\d{2}:\d{2}$/)) return dateStr;
    }

    const date = new Date(dateStr);
    if (isNaN(date.getTime())) return isoStr;

    const now = new Date();
    const diffSec = Math.floor((now.getTime() - date.getTime()) / 1000);
    const i18n = (I18N_DATA as any)[lang] || (I18N_DATA as any)['en'];

    // Relative time for same day
    if (diffSec < 86400 && date.getDate() === now.getDate()) {
        if (diffSec < 60) return i18n.justNow || '방금 전';
        
        const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'always', style: 'short' });
        if (diffSec < 3600) {
            return rtf.format(-Math.floor(diffSec / 60), 'minute');
        }
        return rtf.format(-Math.floor(diffSec / 3600), 'hour');
    }

    const isSameYear = date.getFullYear() === now.getFullYear();
    const options: Intl.DateTimeFormatOptions = {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
    };

    if (!isSameYear) options.year = 'numeric';

    const yesterday = new Date(now);
    yesterday.setDate(now.getDate() - 1);
    const isYesterday = date.toDateString() === yesterday.toDateString();

    if (isYesterday) {
        const timePart = date.toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit', hour12: false });
        return `${i18n.yesterday || '어제'} ${timePart}`;
    }

    return new Intl.DateTimeFormat(lang, options).format(date).replace(',', '');
}

/**
 * Escapes HTML special characters.
 */
export function escapeHTML(str: string | null | undefined): string {
    if (!str) return '';
    const htmlEntities: Record<string, string> = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        "'": '&#39;',
        '"': '&quot;'
    };
    return String(str).replace(/[&<>'"]/g, tag => htmlEntities[tag] || tag);
}

/**
 * Tab system setup with BEM support.
 */
export function setupTabs(
    tabSelector: string,
    contentSelector: string,
    attrName: string = 'id',
    activeClass: string = 'active',
    onSwitch?: (id: string) => void
): void {
    const tabs = document.querySelectorAll(tabSelector);
    const contents = document.querySelectorAll(contentSelector);

    const switchTab = (tabId: string | null): void => {
        if (!tabId) return;
        tabs.forEach(b => b.classList.toggle(activeClass, b.getAttribute(attrName) === tabId));
        contents.forEach(c => {
            const isActive = c.id === tabId;
            c.classList.toggle('active', isActive);
            
            // Support BEM Modifier: Toggle --active on block class
            for (let i = 0; i < c.classList.length; i++) {
                const cls = c.classList[i];
                if (cls.startsWith('c-') && !cls.includes('--')) {
                    c.classList.toggle(`${cls}--active`, isActive);
                    break;
                }
            }
        });
    };

    tabs.forEach(btn => {
        btn.addEventListener('click', () => {
            const targetId = btn.getAttribute(attrName);
            if (targetId) {
                switchTab(targetId);
                if (onSwitch) onSwitch(targetId);
            }
        });
    });
};
