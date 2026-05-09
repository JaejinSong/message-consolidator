// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

class MockIO {
    constructor(public cb: IntersectionObserverCallback) {}
    observe = vi.fn();
    disconnect = vi.fn();
    unobserve = vi.fn();
    takeRecords = (): IntersectionObserverEntry[] => [];
    readonly root: Element | Document | null = null;
    readonly rootMargin: string = '';
    readonly thresholds: ReadonlyArray<number> = [];
}
(globalThis as unknown as Record<string, unknown>)['IntersectionObserver'] = MockIO;

const FULL_GUIDE_HTML = `
    <div id="guideSection">
        <div data-guide-sidebar></div>
        <div data-guide-search></div>
        <div data-guide-toc></div>
        <div id="guideContent"></div>
    </div>
`;

describe('guide index', () => {
    beforeEach(() => {
        vi.resetModules();
        document.body.innerHTML = FULL_GUIDE_HTML;
        Element.prototype.scrollIntoView = vi.fn();
        Object.defineProperty(window, 'location', {
            writable: true,
            configurable: true,
            value: { hash: '', origin: 'http://localhost', pathname: '/' },
        });
        vi.spyOn(history, 'replaceState').mockImplementation(() => undefined);
        vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    async function importGuide() {
        const mod = await import('../index');
        return mod.guide;
    }

    it('init() returns early without throw when #guideSection is absent', async () => {
        document.body.innerHTML = '';
        const guide = await importGuide();
        expect(() => guide.init()).not.toThrow();
    });

    it('init() mounts sidebar buttons inside data-guide-sidebar container', async () => {
        const guide = await importGuide();
        guide.init();
        const sidebarContainer = document.querySelector('[data-guide-sidebar]')!;
        expect(sidebarContainer.querySelectorAll('button').length).toBeGreaterThan(0);
    });

    it('init() is idempotent: calling twice does not double-mount buttons', async () => {
        const guide = await importGuide();
        guide.init();
        const countAfterFirst = document.querySelector('[data-guide-sidebar]')!.querySelectorAll('button').length;
        guide.init();
        const countAfterSecond = document.querySelector('[data-guide-sidebar]')!.querySelectorAll('button').length;
        expect(countAfterFirst).toBe(countAfterSecond);
    });

    it('onShow() with empty hash renders getting-started and calls replaceState', async () => {
        (window.location as unknown as Record<string, string>).hash = '';
        const guide = await importGuide();
        guide.init();
        guide.onShow();

        const content = document.getElementById('guideContent')!;
        expect(content.innerHTML.length).toBeGreaterThan(0);
        expect(history.replaceState).toHaveBeenCalledWith(null, '', '#guide/getting-started');
    });

    it('onShow() with #guide/channels hash renders channels content', async () => {
        (window.location as unknown as Record<string, string>).hash = '#guide/channels';
        const guide = await importGuide();
        guide.init();
        guide.onShow();

        const content = document.getElementById('guideContent')!;
        // parseMarkdown is mocked: output is <p>{markdown text}</p>; just verify non-empty
        expect(content.innerHTML.length).toBeGreaterThan(0);
    });

    it('navigateTo updates location hash to the target section', async () => {
        const guide = await importGuide();
        guide.init();
        guide.onShow();

        guide.navigateTo('reports');
        expect((window.location as unknown as Record<string, string>).hash).toContain('reports');
    });

    it('navigateTo same section+heading does not re-render (heading-only nav)', async () => {
        (window.location as unknown as Record<string, string>).hash = '#guide/reports';
        const guide = await importGuide();
        guide.init();
        guide.onShow();

        const content = document.getElementById('guideContent')!;
        const sentinel = 'SENTINEL_VALUE';
        content.innerHTML = sentinel;

        // navigateTo same section with a heading — should not overwrite content
        guide.navigateTo('reports', 'token-usage');

        expect(content.innerHTML).toBe(sentinel);
    });
});
