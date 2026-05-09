import type { GuideSection } from './content';

export interface TocController {
    rebuild(): void;
    setActive(headingId: string): void;
    scrollToHeading(headingId: string): void;
    destroy(): void;
}

function slugify(text: string): string {
    return text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
}

function ensureId(el: HTMLElement, usedIds: Set<string>): string {
    if (el.id) return el.id;
    let base = slugify(el.textContent ?? '');
    if (!base) base = 'heading';
    let candidate = base;
    let counter = 1;
    while (usedIds.has(candidate)) {
        candidate = `${base}-${counter++}`;
    }
    usedIds.add(candidate);
    el.id = candidate;
    return candidate;
}

export function mountToc(
    container: HTMLElement,
    contentEl: HTMLElement,
    onHeadingActivate: (headingId: string) => void,
    getSectionRef: () => GuideSection | null,
): TocController {
    let observer: IntersectionObserver | null = null;
    let activeId: string | null = null;

    function getLinks(): NodeListOf<HTMLAnchorElement> {
        return container.querySelectorAll<HTMLAnchorElement>('a[data-heading]');
    }

    function disconnectObserver(): void {
        if (observer) {
            observer.disconnect();
            observer = null;
        }
    }

    function rebuild(): void {
        disconnectObserver();
        activeId = null;

        const headings = Array.from(contentEl.querySelectorAll<HTMLElement>('h2, h3'));
        const usedIds = new Set<string>();
        headings.forEach(h => ensureId(h, usedIds));

        const nav = document.createElement('nav');
        nav.className = 'c-guide__toc';
        nav.setAttribute('aria-label', 'On this page');

        const rootUl = document.createElement('ul');
        let currentH2Li: HTMLLIElement | null = null;
        let subUl: HTMLUListElement | null = null;

        headings.forEach(h => {
            const id = h.id;
            const section = getSectionRef() ?? '';
            const href = section ? `#guide/${section}/${id}` : `#${id}`;

            const a = document.createElement('a');
            a.href = href;
            a.setAttribute('data-heading', id);
            a.textContent = h.textContent ?? '';
            a.addEventListener('click', (e: MouseEvent) => {
                e.preventDefault();
                onHeadingActivate(id);
            });

            const li = document.createElement('li');
            li.appendChild(a);

            if (h.tagName === 'H2') {
                subUl = null;
                currentH2Li = li;
                rootUl.appendChild(li);
            } else {
                if (!subUl) {
                    subUl = document.createElement('ul');
                    subUl.className = 'c-guide__toc-sub';
                    if (currentH2Li) {
                        currentH2Li.appendChild(subUl);
                    } else {
                        rootUl.appendChild(subUl);
                    }
                }
                li.classList.add('c-guide__toc-item--sub');
                subUl.appendChild(li);
            }
        });

        nav.appendChild(rootUl);
        container.innerHTML = '';
        container.appendChild(nav);

        if (headings.length === 0) return;

        observer = new IntersectionObserver(
            (entries) => {
                const intersecting = entries
                    .filter(e => e.isIntersecting)
                    .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
                if (intersecting.length === 0) return;
                const topId = (intersecting[0].target as HTMLElement).id;
                if (topId && topId !== activeId) {
                    activeId = topId;
                    onHeadingActivate(topId);
                }
            },
            { rootMargin: '-15% 0px -70% 0px' },
        );

        headings.forEach(h => observer!.observe(h));
    }

    return {
        rebuild,

        setActive(headingId: string): void {
            getLinks().forEach(a => {
                a.classList.toggle(
                    'c-guide__toc-item--active',
                    a.getAttribute('data-heading') === headingId,
                );
            });
        },

        scrollToHeading(headingId: string): void {
            const el = contentEl.querySelector<HTMLElement>(`#${CSS.escape(headingId)}`);
            if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
        },

        destroy(): void {
            disconnectObserver();
            container.innerHTML = '';
        },
    };
}
