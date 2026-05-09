// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { parseGuideHash, serializeGuideRoute, GuideRouter } from '../router';
import { GUIDE_SECTIONS } from '../content';
import type { GuideRoute } from '../router';
import type { Mock } from 'vitest';

const ALL_SECTIONS = GUIDE_SECTIONS.map(s => s.key);

describe('parseGuideHash', () => {
    it('returns null for empty string', () => {
        expect(parseGuideHash('')).toBeNull();
    });

    it('returns null for non-guide hash', () => {
        expect(parseGuideHash('#settings')).toBeNull();
    });

    it('returns null for malformed guide hash with no section', () => {
        expect(parseGuideHash('#guide/')).toBeNull();
    });

    it('returns null for unknown section', () => {
        expect(parseGuideHash('#guide/unknown-section')).toBeNull();
    });

    it('parses #guide/channels', () => {
        const result = parseGuideHash('#guide/channels');
        expect(result).toEqual({ section: 'channels', heading: undefined });
    });

    it('parses #guide/channels/automation with heading', () => {
        const result = parseGuideHash('#guide/channels/automation');
        expect(result).toEqual({ section: 'channels', heading: 'automation' });
    });

    it('parses all valid sections', () => {
        ALL_SECTIONS.forEach(key => {
            const result = parseGuideHash(`#guide/${key}`);
            expect(result).not.toBeNull();
            expect(result?.section).toBe(key);
        });
    });
});

describe('serializeGuideRoute', () => {
    it('serializes route without heading', () => {
        const route: GuideRoute = { section: 'channels' };
        expect(serializeGuideRoute(route)).toBe('#guide/channels');
    });

    it('serializes route with heading', () => {
        const route: GuideRoute = { section: 'channels', heading: 'automation' };
        expect(serializeGuideRoute(route)).toBe('#guide/channels/automation');
    });

    it('round-trips with parseGuideHash for all sections without heading', () => {
        ALL_SECTIONS.forEach(key => {
            const route: GuideRoute = { section: key };
            const serialized = serializeGuideRoute(route);
            const parsed = parseGuideHash(serialized);
            expect(parsed?.section).toBe(key);
            expect(parsed?.heading).toBeUndefined();
        });
    });

    it('round-trips with parseGuideHash for all sections with heading', () => {
        ALL_SECTIONS.forEach(key => {
            const route: GuideRoute = { section: key, heading: 'some-heading' };
            const serialized = serializeGuideRoute(route);
            const parsed = parseGuideHash(serialized);
            expect(parsed?.section).toBe(key);
            expect(parsed?.heading).toBe('some-heading');
        });
    });
});

describe('GuideRouter', () => {
    let onChangeMock: Mock<(route: GuideRoute) => void>;
    let router: GuideRouter;

    beforeEach(() => {
        vi.spyOn(window, 'addEventListener').mockImplementation(
            (_event: string, _handler: EventListenerOrEventListenerObject) => undefined,
        );
        vi.spyOn(window, 'removeEventListener').mockImplementation(() => undefined);

        Object.defineProperty(window, 'location', {
            writable: true,
            value: { hash: '' },
        });

        vi.spyOn(history, 'replaceState').mockImplementation(() => undefined);

        onChangeMock = vi.fn<(route: GuideRoute) => void>();
        router = new GuideRouter(onChangeMock);
        router.start();
    });

    afterEach(() => {
        router.stop();
        vi.restoreAllMocks();
    });

    it('triggers onChange on section change', () => {
        router.push({ section: 'tasks' });
        expect(onChangeMock).toHaveBeenCalledTimes(1);
        expect(onChangeMock).toHaveBeenCalledWith({ section: 'tasks', heading: undefined });
    });

    it('does NOT trigger onChange on identical route', () => {
        router.push({ section: 'tasks' });
        onChangeMock.mockClear();
        router.push({ section: 'tasks' });
        expect(onChangeMock).not.toHaveBeenCalled();
    });

    it('does NOT trigger onChange for identical route with same heading', () => {
        router.push({ section: 'tasks', heading: 'intro' });
        onChangeMock.mockClear();
        router.push({ section: 'tasks', heading: 'intro' });
        expect(onChangeMock).not.toHaveBeenCalled();
    });

    it('triggers onChange on heading change within same section', () => {
        router.push({ section: 'tasks', heading: 'intro' });
        onChangeMock.mockClear();
        router.push({ section: 'tasks', heading: 'advanced' });
        expect(onChangeMock).toHaveBeenCalledTimes(1);
    });

    it('uses replaceState for heading-only change within same section', () => {
        router.push({ section: 'tasks', heading: 'intro' });
        router.push({ section: 'tasks', heading: 'advanced' });
        expect(history.replaceState).toHaveBeenCalled();
    });

    it('current() returns null before any push', () => {
        expect(router.current()).toBeNull();
    });

    it('current() returns last pushed route', () => {
        router.push({ section: 'faq' });
        expect(router.current()).toEqual({ section: 'faq', heading: undefined });
    });
});
