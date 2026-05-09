import { GUIDE_SECTIONS, GUIDE_CONTENT } from './content';
import type { GuideSection } from './content';
import { GuideRouter, parseGuideHash, serializeGuideRoute } from './router';
import type { GuideRoute } from './router';
import { mountSidebar } from './sidebar';
import type { SidebarController } from './sidebar';
import { mountToc } from './toc';
import type { TocController } from './toc';
import { mountSearch } from './search';
import type { SearchController } from './search';
import { applyHeadingAnchors } from './anchors';
import { parseMarkdown } from '../logic';

export type { GuideSection } from './content';

export interface GuideAPI {
    init(): void;
    onShow(): void;
    navigateTo(section: GuideSection, headingId?: string): void;
}

// Module-private state
let router: GuideRouter | null = null;
let sidebar: SidebarController | null = null;
let toc: TocController | null = null;
// Why: kept for module lifetime — destroy path would call searchCtrl?.destroy()
const controllers: { search: SearchController | null } = { search: null };
let guideContentEl: HTMLElement | null = null;
let currentSection: GuideSection | null = null;
let initialized = false;

function renderRoute(route: GuideRoute): void {
    if (!guideContentEl) return;

    if (route.section !== currentSection) {
        guideContentEl.innerHTML = parseMarkdown(GUIDE_CONTENT[route.section]);
        currentSection = route.section;

        applyHeadingAnchors(guideContentEl, () => currentSection ?? 'getting-started');

        toc?.rebuild();
        sidebar?.setActive(route.section);

        // Why: page itself scrolls (no inner overflow on content); reset window position.
        window.scrollTo({ top: 0, behavior: 'auto' });
    }

    if (route.heading) {
        requestAnimationFrame(() => {
            toc?.scrollToHeading(route.heading!);
        });
        toc?.setActive(route.heading);
    }
}

export const guide: GuideAPI = {
    init(): void {
        if (initialized) return;

        const guideSection = document.getElementById('guideSection');
        if (!guideSection) return;

        guideContentEl = document.getElementById('guideContent');
        if (!guideContentEl) return;

        const sidebarContainer = guideSection.querySelector<HTMLElement>('[data-guide-sidebar]');
        const tocContainer = guideSection.querySelector<HTMLElement>('[data-guide-toc]');
        const searchContainer = guideSection.querySelector<HTMLElement>('[data-guide-search]');

        if (sidebarContainer) {
            sidebar = mountSidebar(sidebarContainer, GUIDE_SECTIONS, (section) => {
                guide.navigateTo(section);
            });
        }

        if (searchContainer) {
            controllers.search = mountSearch(
                searchContainer,
                GUIDE_SECTIONS,
                GUIDE_CONTENT,
                (section, headingId) => {
                    guide.navigateTo(section, headingId);
                },
            );
        }

        if (tocContainer && guideContentEl) {
            toc = mountToc(
                tocContainer,
                guideContentEl,
                (headingId) => {
                    toc?.setActive(headingId);
                    if (currentSection) {
                        router?.push({ section: currentSection, heading: headingId });
                    }
                },
                () => currentSection,
            );
        }

        router = new GuideRouter((route) => {
            renderRoute(route);
        });
        router.start();

        initialized = true;
    },

    onShow(): void {
        const parsed = parseGuideHash(location.hash);

        if (parsed) {
            renderRoute(parsed);
            return;
        }

        if (!currentSection) {
            const defaultRoute: GuideRoute = { section: 'getting-started' };
            renderRoute(defaultRoute);
            history.replaceState(null, '', serializeGuideRoute(defaultRoute));
        }
    },

    navigateTo(section: GuideSection, headingId?: string): void {
        const route: GuideRoute = { section, heading: headingId };
        router?.push(route);
    },
};
