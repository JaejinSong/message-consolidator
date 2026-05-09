// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { Mock } from 'vitest';
import { applyHeadingAnchors } from '../anchors';
import type { GuideSection } from '../content';

describe('applyHeadingAnchors', () => {
    let contentEl: HTMLElement;
    let getCurrentSection: Mock<() => GuideSection>;

    beforeEach(() => {
        vi.useFakeTimers();
        document.body.innerHTML = '';
        contentEl = document.createElement('div');
        document.body.appendChild(contentEl);
        getCurrentSection = vi.fn<() => GuideSection>().mockReturnValue('channels');

        Object.defineProperty(navigator, 'clipboard', {
            writable: true,
            configurable: true,
            value: { writeText: vi.fn().mockResolvedValue(undefined) },
        });
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    function buildHeadings(): void {
        contentEl.innerHTML = `
            <h2 id="overview">Overview</h2>
            <h3 id="details">Details</h3>
        `;
    }

    it('appends one anchor button to each h2 and h3', () => {
        buildHeadings();
        applyHeadingAnchors(contentEl, getCurrentSection);
        const btns = contentEl.querySelectorAll('.c-guide__heading-anchor');
        expect(btns.length).toBe(2);
    });

    it('is idempotent: second call does not duplicate buttons', () => {
        buildHeadings();
        applyHeadingAnchors(contentEl, getCurrentSection);
        applyHeadingAnchors(contentEl, getCurrentSection);
        const btns = contentEl.querySelectorAll('.c-guide__heading-anchor');
        expect(btns.length).toBe(2);
    });

    it('click calls navigator.clipboard.writeText with correct url', async () => {
        buildHeadings();
        applyHeadingAnchors(contentEl, getCurrentSection);

        const btn = contentEl.querySelector<HTMLButtonElement>('#overview .c-guide__heading-anchor')!;
        btn.click();
        await Promise.resolve();

        const expectedUrl = `${location.origin}${location.pathname}#guide/channels/overview`;
        expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expectedUrl);
    });

    it('applies --copied modifier class after click', async () => {
        buildHeadings();
        applyHeadingAnchors(contentEl, getCurrentSection);

        const btn = contentEl.querySelector<HTMLButtonElement>('#overview .c-guide__heading-anchor')!;
        btn.click();
        await Promise.resolve();

        expect(btn.classList.contains('c-guide__heading-anchor--copied')).toBe(true);
    });

    it('removes --copied modifier after ~1400ms', async () => {
        buildHeadings();
        applyHeadingAnchors(contentEl, getCurrentSection);

        const btn = contentEl.querySelector<HTMLButtonElement>('#overview .c-guide__heading-anchor')!;
        btn.click();
        await Promise.resolve();

        vi.advanceTimersByTime(1500);
        expect(btn.classList.contains('c-guide__heading-anchor--copied')).toBe(false);
    });

    it('falls back to document.execCommand when navigator.clipboard is missing', () => {
        buildHeadings();

        // Define execCommand since happy-dom omits it
        const execCommandMock = vi.fn().mockReturnValue(true);
        Object.defineProperty(document, 'execCommand', {
            writable: true,
            configurable: true,
            value: execCommandMock,
        });

        // Override clipboard to undefined so the falsy branch runs
        Object.defineProperty(navigator, 'clipboard', {
            writable: true,
            configurable: true,
            value: undefined,
        });

        applyHeadingAnchors(contentEl, getCurrentSection);
        const btn = contentEl.querySelector<HTMLButtonElement>('#overview .c-guide__heading-anchor')!;

        expect(() => btn.click()).not.toThrow();
        expect(execCommandMock).toHaveBeenCalledWith('copy');
    });

    it('does not throw when clipboard.writeText rejects', async () => {
        buildHeadings();
        (navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('denied'));

        const execCommandMock = vi.fn().mockReturnValue(true);
        Object.defineProperty(document, 'execCommand', {
            writable: true,
            configurable: true,
            value: execCommandMock,
        });

        applyHeadingAnchors(contentEl, getCurrentSection);
        const btn = contentEl.querySelector<HTMLButtonElement>('#overview .c-guide__heading-anchor')!;

        btn.click();
        await Promise.resolve();
        await Promise.resolve();

        // No uncaught rejection — feedback still applied via fallback
        expect(btn.classList.contains('c-guide__heading-anchor--copied')).toBe(true);
    });
});
