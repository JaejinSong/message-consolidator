import type { GuideSection } from './content';
import { GUIDE_SECTIONS } from './content';

export interface GuideRoute {
    section: GuideSection;
    heading?: string;
}

const VALID_SECTIONS = new Set<string>(GUIDE_SECTIONS.map(s => s.key));

function isValidSection(s: string): s is GuideSection {
    return VALID_SECTIONS.has(s);
}

function routesEqual(a: GuideRoute, b: GuideRoute): boolean {
    return a.section === b.section && a.heading === b.heading;
}

export function parseGuideHash(hash: string): GuideRoute | null {
    if (!hash.startsWith('#guide/')) return null;
    const rest = hash.slice('#guide/'.length);
    const [section, heading] = rest.split('/');
    if (!section || !isValidSection(section)) return null;
    return { section, heading: heading || undefined };
}

export function serializeGuideRoute(route: GuideRoute): string {
    if (route.heading) return `#guide/${route.section}/${route.heading}`;
    return `#guide/${route.section}`;
}

export class GuideRouter {
    private readonly onChange: (route: GuideRoute) => void;
    private currentRoute: GuideRoute | null = null;
    private readonly handler: () => void;

    constructor(onChange: (route: GuideRoute) => void) {
        this.onChange = onChange;
        this.handler = () => {
            const parsed = parseGuideHash(location.hash);
            if (!parsed) return;
            if (this.currentRoute && routesEqual(this.currentRoute, parsed)) return;
            this.currentRoute = parsed;
            this.onChange(parsed);
        };
    }

    start(): void {
        window.addEventListener('hashchange', this.handler);
    }

    stop(): void {
        window.removeEventListener('hashchange', this.handler);
    }

    push(route: GuideRoute): void {
        if (this.currentRoute && routesEqual(this.currentRoute, route)) return;

        const sameSection = this.currentRoute?.section === route.section;
        const serialized = serializeGuideRoute(route);

        if (sameSection) {
            // Why: heading-only navigation is history-neutral
            history.replaceState(null, '', serialized);
        } else {
            location.hash = serialized.slice(1);
        }

        this.currentRoute = route;
        this.onChange(route);
    }

    current(): GuideRoute | null {
        return this.currentRoute;
    }
}
