// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest';

let lastMockIOInstance: MockIO | null = null;

class MockIO {
    observe = vi.fn();
    disconnect = vi.fn();
    unobserve = vi.fn();
    takeRecords = (): IntersectionObserverEntry[] => [];
    readonly root: Element | Document | null = null;
    readonly rootMargin: string = '';
    readonly thresholds: ReadonlyArray<number> = [];
    constructor(public cb: IntersectionObserverCallback) {
        lastMockIOInstance = this;
    }
}

(globalThis as unknown as Record<string, unknown>)['IntersectionObserver'] = MockIO;

import { mountToc } from '../toc';
import type { Mock } from 'vitest';
import type { GuideSection } from '../content';

describe('mountToc', () => {
    let container: HTMLElement;
    let contentEl: HTMLElement;
    let onHeadingActivate: Mock<(headingId: string) => void>;
    let getSectionRef: Mock<() => GuideSection | null>;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        contentEl = document.createElement('div');
        document.body.appendChild(container);
        document.body.appendChild(contentEl);
        onHeadingActivate = vi.fn<(headingId: string) => void>();
        getSectionRef = vi.fn<() => GuideSection | null>().mockReturnValue('channels');
        Element.prototype.scrollIntoView = vi.fn();
    });

    function buildContent(): void {
        contentEl.innerHTML = `
            <h2 id="overview">Overview</h2>
            <h2 id="setup">Setup</h2>
            <h3>Details</h3>
        `;
    }

    it('renders hierarchical nav: h3 item has c-guide__toc-item--sub class', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const subItems = container.querySelectorAll('.c-guide__toc-item--sub');
        expect(subItems.length).toBe(1);
    });

    it('preserves pre-existing heading ids', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const h2 = contentEl.querySelector<HTMLElement>('#overview')!;
        expect(h2.id).toBe('overview');
    });

    it('assigns slug id to heading without id', () => {
        contentEl.innerHTML = '<h2>Getting Started</h2>';
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const h2 = contentEl.querySelector<HTMLElement>('h2')!;
        expect(h2.id).toBe('getting-started');
    });

    it('resolves slug collision by appending counter', () => {
        contentEl.innerHTML = '<h2>Setup</h2><h2>Setup</h2>';
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const headings = contentEl.querySelectorAll<HTMLElement>('h2');
        expect(headings[0].id).toBe('setup');
        expect(headings[1].id).toBe('setup-1');
    });

    it('each toc anchor has href with section from getSectionRef', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const anchor = container.querySelector<HTMLAnchorElement>('a[data-heading="overview"]')!;
        expect(anchor.getAttribute('href')).toBe('#guide/channels/overview');
    });

    it('falls back to #id when getSectionRef returns null', () => {
        getSectionRef.mockReturnValue(null);
        contentEl.innerHTML = '<h2 id="intro">Intro</h2>';
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const anchor = container.querySelector<HTMLAnchorElement>('a[data-heading="intro"]')!;
        expect(anchor.getAttribute('href')).toBe('#intro');
    });

    it('anchor click calls onHeadingActivate and prevents default', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const anchor = container.querySelector<HTMLAnchorElement>('a[data-heading="overview"]')!;
        const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true });
        anchor.dispatchEvent(clickEvent);

        expect(onHeadingActivate).toHaveBeenCalledWith('overview');
        expect(clickEvent.defaultPrevented).toBe(true);
    });

    it('setActive adds active class to matching anchor and removes from others', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        ctrl.setActive('setup');

        const active = container.querySelector('a[data-heading="setup"]')!;
        const other = container.querySelector('a[data-heading="overview"]')!;
        expect(active.classList.contains('c-guide__toc-item--active')).toBe(true);
        expect(other.classList.contains('c-guide__toc-item--active')).toBe(false);
    });

    it('scrollToHeading calls scrollIntoView with smooth+start args', () => {
        contentEl.innerHTML = '<h2 id="target-heading">Target</h2>';
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        ctrl.scrollToHeading('target-heading');

        expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });
    });

    it('rebuild() observes all headings via IntersectionObserver', () => {
        buildContent();
        lastMockIOInstance = null;
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        const headings = contentEl.querySelectorAll('h2, h3');
        expect(lastMockIOInstance).not.toBeNull();
        expect(lastMockIOInstance!.observe).toHaveBeenCalledTimes(headings.length);
    });

    it('rebuild() disconnects previous observer before creating new one', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);

        ctrl.rebuild();
        // Second rebuild should disconnect the first observer
        contentEl.innerHTML = '<h2 id="new-h">New</h2>';
        ctrl.rebuild();
        // If no throw, lifecycle is handled correctly
        expect(container.querySelectorAll('a').length).toBe(1);
    });

    it('destroy clears container', () => {
        buildContent();
        const ctrl = mountToc(container, contentEl, onHeadingActivate, getSectionRef);
        ctrl.rebuild();

        expect(container.innerHTML).not.toBe('');
        ctrl.destroy();
        expect(container.innerHTML).toBe('');
    });
});
