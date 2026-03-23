import { I18N_DATA } from './locales.js';

/**
 * 비동기 함수의 에러 핸들링을 공통화하는 고차 함수 (Higher-Order Function)
 * 중복되는 try-catch 블록을 제거하고, 호출부의 가독성을 높입니다.
 * @param {Function} fn - 실행할 비동기 함수
 * @param {Function} [onError] - 에러 발생 시 실행할 커스텀 롤백 함수 (선택 사항)
 */
export const safeAsync = (fn, onError) => async function (...args) {
    try {
        return await fn.apply(this, args);
    } catch (e) {
        console.error('[Async Error]', e);
        if (e.isAuthError) {
            console.warn('[safeAsync] AuthError detected. Attempting to show login overlay.');
            const overlay = document.getElementById('loginOverlay');
            if (overlay) {
                overlay.classList.remove('hidden');
                console.info('[safeAsync] Login overlay shown successfully.');
            } else {
                console.error('[safeAsync] Login overlay element NOT FOUND in document.');
            }
        }
        if (onError) onError(e);
        throw e; // Rethrow to allow caller to handle if needed
    }
};

/**
 * 날짜 문자열을 다국어가 지원되는 상대 시간 포맷(방금 전, n분 전 등) 또는 지정된 시간대로 변환합니다.
 * @param {string} isoStr - 변환할 날짜 문자열
 * @param {string} lang - 현재 설정된 언어 코드 (예: 'ko', 'en')
 * @returns {string} 포맷팅된 시간 문자열
 */
export const formatDisplayTime = (isoStr, lang) => {
    if (!isoStr) return '-';

    let dateStr = isoStr;
    // Handle legacy suffix from database
    if (typeof dateStr === 'string') {
        if (dateStr.includes(' KST')) dateStr = dateStr.replace(' KST', ' +0900');
        else if (dateStr.includes(' JKT')) dateStr = dateStr.replace(' JKT', ' +0700');
        else if (dateStr.includes(' ICT')) dateStr = dateStr.replace(' ICT', ' +0700');
        else if (dateStr.match(/^\d{2}:\d{2}$/)) {
            return dateStr;
        }
    }

    try {
        const date = new Date(dateStr);
        if (isNaN(date.getTime())) return isoStr;

        // 상대 시간 계산 (당일 24시간 이내)
        const now = new Date();
        const diffMs = now - date;
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMins / 60);
        const i18n = I18N_DATA[lang] || I18N_DATA['en'];

        if (diffHours < 24 && now.getDate() === date.getDate()) {
            if (diffMins < 1) return i18n.justNow || '방금 전';
            if (diffMins < 60) return (i18n.minAgo || '{n}m ago').replace('{n}', diffMins);
            return (i18n.hourAgo || '{n}h ago').replace('{n}', diffHours);
        }

        const yesterdayDate = new Date(now);
        yesterdayDate.setDate(now.getDate() - 1);
        const isYesterday = (date.getDate() === yesterdayDate.getDate() &&
            date.getMonth() === yesterdayDate.getMonth() &&
            date.getFullYear() === yesterdayDate.getFullYear());

        const hh = String(date.getHours()).padStart(2, '0');
        const min = String(date.getMinutes()).padStart(2, '0');

        if (isYesterday) {
            const ydayLabel = i18n.yesterday || '어제';
            return `${ydayLabel} ${hh}:${min}`;
        }

        // 7일 이내면 요일 표시
        const diffDays = Math.floor(diffHours / 24);
        if (diffDays < 7) {
            const dayNames = i18n.dayNames || ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
            const dayName = dayNames[date.getDay()];
            return `${dayName} ${hh}:${min}`;
        }

        // 그 외엔 MM-DD HH:mm (타임존 제거하여 공간 확보)
        const mm = String(date.getMonth() + 1).padStart(2, '0');
        const dd = String(date.getDate()).padStart(2, '0');

        return `${mm}-${dd} ${hh}:${min}`;
    } catch (e) {
        return isoStr;
    }
};

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