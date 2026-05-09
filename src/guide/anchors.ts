import type { GuideSection } from './content';

const COPIED_MODIFIER = 'c-guide__heading-anchor--copied';
const COPIED_DURATION_MS = 1400;

function fallbackCopy(text: string): void {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    try {
        document.execCommand('copy');
    } catch {
        // Why: silent failure — clipboard unavailable in this context
    } finally {
        document.body.removeChild(textarea);
    }
}

// Why: idempotent — toc.ts assigns ids before render; calling either first is safe.
// Both ensure id before operating on the heading.
export function applyHeadingAnchors(
    contentEl: HTMLElement,
    getCurrentSection: () => GuideSection,
): void {
    const headings = contentEl.querySelectorAll<HTMLElement>('h2, h3');

    headings.forEach(heading => {
        // Skip if anchor button already applied
        if (heading.querySelector('.c-guide__heading-anchor')) return;

        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'c-guide__heading-anchor';
        btn.setAttribute('aria-label', 'Copy link to section');
        btn.textContent = '🔗';

        btn.addEventListener('click', () => {
            const section = getCurrentSection();
            const url = `${location.origin}${location.pathname}#guide/${section}/${heading.id}`;

            const applyFeedback = (): void => {
                btn.classList.add(COPIED_MODIFIER);
                setTimeout(() => btn.classList.remove(COPIED_MODIFIER), COPIED_DURATION_MS);
            };

            if (navigator.clipboard) {
                navigator.clipboard.writeText(url).then(applyFeedback).catch(() => {
                    fallbackCopy(url);
                    applyFeedback();
                });
            } else {
                fallbackCopy(url);
                applyFeedback();
            }
        });

        heading.appendChild(btn);
    });
}
