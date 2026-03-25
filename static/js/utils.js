import { I18N_DATA } from './locales.js';

/**
 * 비동기 함수의 에러 핸들링을 공통화하는 고차 함수 (Higher-Order Function)
 * @param {Function} fn - 실행할 비동기 함수
 * @param {Object} [options] - 옵션
 * @param {boolean} [options.triggerAuthOverlay=false] - 인증 에러 시 로그인 오버레이 노출 여부
 * @param {Function} [options.onError] - 에러 발생 시 커스텀 핸들러
 */
export const safeAsync = (fn, options = {}) => async function (...args) {
    const { triggerAuthOverlay = false, onError } = options;
    try {
        return await fn.apply(this, args);
    } catch (e) {
        console.error('[Async Error]', e);
        if (e.isAuthError && triggerAuthOverlay) {
            // Defensive check: only show overlay if we truly lack a session hint
            if (!hasSessionHint()) {
                console.warn('[safeAsync] AuthError and no session hint. Triggering login overlay.');
                const overlay = document.getElementById('loginOverlay');
                if (overlay) {
                    overlay.classList.remove('hidden');
                    overlay.style.display = 'flex'; // Explicitly show
                }
            } else {
                console.warn('[safeAsync] AuthError detected but session hint exists. Ignoring overlay trigger.');
            }
        }
        if (onError) onError(e);
        throw e;
    }
};

/**
 * Checks if the user is likely authenticated based on the non-HttpOnly hint cookie.
 * @returns {boolean}
 */
export const hasSessionHint = () => {
    return document.cookie.split(';').some(item => item.trim().startsWith('session_active=true'));
};

/**
 * TimeService provides a centralized system for all date/time operations.
 */
export const TimeService = {
    /**
     * Returns a 'YYYY-MM-DD' string for the user's local timezone.
     */
    getLocalDateString(date = new Date()) {
        return date.toLocaleDateString('en-CA');
    },

    /**
     * Calculates the absolute difference in days between two dates.
     */
    getDiffInDays(date1, date2) {
        return Math.floor(Math.abs(date1 - date2) / (1000 * 60 * 60 * 24));
    },

    /**
     * Formats a date string into a user-friendly display string using Intl APIs.
     * Supports relative time (e.g., '3m ago') and absolute time (e.g., 'MM-DD HH:mm').
     */
    formatDisplayTime(isoStr, lang = 'en') {
        if (!isoStr) return '-';

        let dateStr = isoStr;
        // Legacy handling
        if (typeof dateStr === 'string') {
            dateStr = dateStr.replace(' KST', ' +0900').replace(' JKT', ' +0700').replace(' ICT', ' +0700');
            if (dateStr.match(/^\d{2}:\d{2}$/)) return dateStr;
        }

        const date = new Date(dateStr);
        if (isNaN(date.getTime())) return isoStr;

        const now = new Date();
        const diffSec = Math.floor((now - date) / 1000);
        const i18n = I18N_DATA[lang] || I18N_DATA['en'];

        // Use Intl.RelativeTimeFormat for recent times (within 24 hours)
        if (diffSec < 86400 && date.getDate() === now.getDate()) {
            if (diffSec < 60) return i18n.justNow || '방금 전';
            
            const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'always', style: 'short' });
            if (diffSec < 3600) {
                return rtf.format(-Math.floor(diffSec / 60), 'minute');
            }
            return rtf.format(-Math.floor(diffSec / 3600), 'hour');
        }

        // Yesterday and older
        const isSameYear = date.getFullYear() === now.getFullYear();
        const options = {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            hour12: false
        };

        if (!isSameYear) options.year = 'numeric';

        // Check if it was "yesterday"
        const yesterday = new Date(now);
        yesterday.setDate(now.getDate() - 1);
        const isYesterday = date.toDateString() === yesterday.toDateString();

        if (isYesterday) {
            const timePart = date.toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit', hour12: false });
            return `${i18n.yesterday || '어제'} ${timePart}`;
        }

        return new Intl.DateTimeFormat(lang, options).format(date).replace(',', '');
    }
};

// Compatibility exports
export const getLocalDateString = TimeService.getLocalDateString;
export const getDiffInDays = TimeService.getDiffInDays;
export const formatDisplayTime = TimeService.formatDisplayTime;

/**
 * XSS 방지를 위해 문자열 내 특수 문자를 HTML 엔티티로 치환합니다.
 * @param {string} str - 원본 문자열
 * @returns {string} 이스케이프 처리된 문자열
 */
export const escapeHTML = (str) => {
    if (!str) return '';
    return String(str).replace(/[&<>'"]/g, tag => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
    }[tag]));
};