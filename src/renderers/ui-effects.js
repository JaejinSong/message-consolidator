import { parseMarkdown } from '../logic.js';
import confetti from 'canvas-confetti';

/**
 * Shows a non-blocking toast notification.
 */
export function showToast(message, type = 'error') {
    const toast = document.createElement('div');
    toast.className = `toast-popup toast-${type}`;

    const existingToasts = document.querySelectorAll('.toast-popup');
    const bottomOffset = 30 + (existingToasts.length * 70);
    toast.style.bottom = `calc(${bottomOffset} / 16 * 1rem)`;

    const icon = document.createElement('span');
    icon.textContent = type === 'error' ? '⚠️' : '✅';
    toast.appendChild(icon);

    const textNode = document.createElement('span');
    textNode.textContent = message;
    toast.appendChild(textNode);

    document.body.appendChild(toast);

    requestAnimationFrame(() => {
        toast.classList.add('show');
    });

    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 400);
    }, 3000);
}

/**
 * Triggers XP animation in the UI.
 */
export function triggerXPAnimation() {
    const overlay = document.getElementById('xpOverlay');
    if (!overlay) return;
    overlay.classList.remove('hidden');
    overlay.style.animation = 'none';
    overlay.offsetHeight; 
    overlay.style.animation = 'xpFloat 1.2s ease-out forwards';
    setTimeout(() => overlay.classList.add('hidden'), 1200);
}

/**
 * Triggers confetti animation.
 */
export function triggerConfetti(type = 'classic') {
    if (typeof confetti !== 'function') return;

    if (type === 'star') {
        confetti({
            particleCount: 100,
            spread: 70,
            origin: { y: 0.6 },
            colors: ['var(--color-warning)', 'var(--color-confetti-gold)', 'var(--white)'],
            shapes: ['star', 'circle']
        });
    } else if (type === 'snow') {
        confetti({
            particleCount: 150,
            spread: 100,
            origin: { y: 0.4 },
            colors: ['var(--white)', 'var(--color-confetti-snow-1)', 'var(--color-confetti-snow-2)'],
            shapes: ['circle'],
            gravity: 0.3,
            scalar: 0.7
        });
    } else {
        confetti({
            particleCount: 100,
            spread: 70,
            origin: { y: 0.6 },
            colors: ['var(--color-confetti-sky)', 'var(--color-info)', 'var(--white)', 'var(--color-confetti-pink)', 'var(--color-confetti-lime)']
        });
    }
}

/**
 * Renders release notes in the modal.
 */
export function renderReleaseNotes(content) {
    const container = document.getElementById('releaseNotesContent');
    if (!container) return;
    container.innerHTML = `<div class="release-notes-markdown">${parseMarkdown(content)}</div>`;
}

/**
 * 스캔 버튼 및 화면의 로딩 상태를 제어합니다.
 */
export function setScanLoading(isLoading) {
    const btn = document.getElementById('scanBtn');
    const scanBtnIcon = document.getElementById('scanBtnIcon');
    const loading = document.getElementById('loading');

    if (btn) btn.disabled = isLoading;
    if (scanBtnIcon) scanBtnIcon.style.animation = isLoading ? 'spin 1s linear infinite' : '';
    if (loading) loading.classList.toggle('active', isLoading);
}

/**
 * 초기 테마 상태를 UI(아이콘 및 Body 클래스)에 적용합니다.
 */
export function setTheme(theme) {
    const isLight = theme === 'light';
    document.body.classList.toggle('light-theme', isLight);
    const themeToggleBtn = document.getElementById('themeToggleBtn');
    if (themeToggleBtn) {
        themeToggleBtn.innerHTML = isLight
            ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: var(--spacing-xl); height: var(--spacing-xl);"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>`
            : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: var(--spacing-xl); height: var(--spacing-xl);"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>`;
    }
}

/**
 * 테마 토글 버튼 클릭 이벤트를 바인딩합니다.
 */
export function bindThemeToggle(onToggle) {
    const themeToggleBtn = document.getElementById('themeToggleBtn');
    if (!themeToggleBtn) return;
    themeToggleBtn.addEventListener('click', () => {
        const isLight = document.body.classList.toggle('light-theme');
        setTheme(isLight ? 'light' : 'dark');
        if (onToggle) onToggle(isLight);
    });
}
