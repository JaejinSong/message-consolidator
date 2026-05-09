import type { GuideSection } from './content';

interface SearchEntry {
    section: GuideSection;
    headingId?: string;
    headingText?: string;
    snippet: string;
    score: number;
}

export interface SearchController {
    destroy(): void;
}

const MIN_PARAGRAPH_LENGTH = 20;
const MAX_RESULTS = 12;
const DEBOUNCE_MS = 150;

function slugify(text: string): string {
    return text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
}

function buildIndex(
    sections: { key: GuideSection; title: string; icon: string }[],
    content: Record<GuideSection, string>,
): SearchEntry[] {
    const entries: SearchEntry[] = [];

    sections.forEach(s => {
        const lines = content[s.key].split('\n');
        let currentHeadingId: string | undefined;
        let currentHeadingText: string | undefined;
        let paragraphLines: string[] = [];

        function flushParagraph(): void {
            if (paragraphLines.length === 0) return;
            const snippet = paragraphLines.join(' ').trim();
            if (snippet.length >= MIN_PARAGRAPH_LENGTH) {
                entries.push({
                    section: s.key,
                    headingId: currentHeadingId,
                    headingText: currentHeadingText,
                    snippet,
                    score: 1,
                });
            }
            paragraphLines = [];
        }

        lines.forEach(line => {
            const h2Match = /^##\s+(.+)$/.exec(line);
            const h3Match = /^###\s+(.+)$/.exec(line);

            if (h2Match || h3Match) {
                flushParagraph();
                const headingText = (h2Match ?? h3Match)![1].trim();
                currentHeadingId = slugify(headingText);
                currentHeadingText = headingText;
                entries.push({
                    section: s.key,
                    headingId: currentHeadingId,
                    headingText,
                    snippet: headingText,
                    score: 2,
                });
            } else if (line.trim().length >= MIN_PARAGRAPH_LENGTH) {
                paragraphLines.push(line.trim());
                if (paragraphLines.length >= 3) flushParagraph();
            } else if (line.trim() === '') {
                flushParagraph();
            }
        });

        flushParagraph();
    });

    return entries;
}

function search(query: string, index: SearchEntry[]): SearchEntry[] {
    const q = query.toLowerCase();
    return index
        .filter(e =>
            (e.headingText?.toLowerCase().includes(q) ?? false) ||
            e.snippet.toLowerCase().includes(q),
        )
        .sort((a, b) => b.score - a.score)
        .slice(0, MAX_RESULTS);
}

export function mountSearch(
    container: HTMLElement,
    sections: { key: GuideSection; title: string; icon: string }[],
    content: Record<GuideSection, string>,
    onResultSelect: (section: GuideSection, headingId?: string) => void,
): SearchController {
    let index: SearchEntry[] | null = null;
    let debounceTimer = 0;
    let selectedIdx = -1;

    const wrapper = document.createElement('div');
    wrapper.className = 'c-guide__search';

    const input = document.createElement('input');
    input.type = 'search';
    input.placeholder = 'Search help...';
    input.setAttribute('aria-label', 'Search help');

    const listbox = document.createElement('ul');
    listbox.className = 'c-guide__search-results';
    listbox.setAttribute('role', 'listbox');
    listbox.hidden = true;

    wrapper.appendChild(input);
    wrapper.appendChild(listbox);
    container.appendChild(wrapper);

    function ensureIndex(): SearchEntry[] {
        if (!index) index = buildIndex(sections, content);
        return index;
    }

    function clearResults(): void {
        listbox.hidden = true;
        listbox.innerHTML = '';
        selectedIdx = -1;
    }

    function getResultItems(): HTMLElement[] {
        return Array.from(listbox.querySelectorAll<HTMLElement>('[role="option"]'));
    }

    function highlightItem(idx: number): void {
        const items = getResultItems();
        items.forEach((item, i) => {
            item.classList.toggle('c-guide__search-result--selected', i === idx);
        });
        selectedIdx = idx;
    }

    function renderResults(results: SearchEntry[]): void {
        listbox.innerHTML = '';

        if (results.length === 0) {
            listbox.hidden = true;
            return;
        }

        let currentSection: GuideSection | null = null;

        results.forEach(entry => {
            if (entry.section !== currentSection) {
                currentSection = entry.section;
                const sep = document.createElement('li');
                sep.setAttribute('role', 'presentation');
                sep.className = 'c-guide__search-sep';
                const sectionDef = sections.find(s => s.key === entry.section);
                sep.textContent = sectionDef ? `${sectionDef.icon} ${sectionDef.title}` : entry.section;
                listbox.appendChild(sep);
            }

            const li = document.createElement('li');
            li.setAttribute('role', 'option');
            li.className = 'c-guide__search-result';
            li.textContent = entry.snippet;

            li.addEventListener('mousedown', (e: MouseEvent) => {
                // Why: mousedown fires before input blur; prevent closing before click registers
                e.preventDefault();
            });

            li.addEventListener('click', () => {
                clearResults();
                input.value = '';
                onResultSelect(entry.section, entry.headingId);
            });

            listbox.appendChild(li);
        });

        listbox.hidden = false;
        selectedIdx = -1;
    }

    function runSearch(): void {
        const q = input.value.trim();
        if (!q) {
            clearResults();
            return;
        }
        renderResults(search(q, ensureIndex()));
    }

    function handleInput(): void {
        clearTimeout(debounceTimer);
        debounceTimer = window.setTimeout(runSearch, DEBOUNCE_MS);
    }

    function handleKeydown(e: KeyboardEvent): void {
        const items = getResultItems();

        switch (e.key) {
            case 'ArrowDown': {
                e.preventDefault();
                const next = Math.min(selectedIdx + 1, items.length - 1);
                highlightItem(next);
                break;
            }
            case 'ArrowUp': {
                e.preventDefault();
                const prev = Math.max(selectedIdx - 1, 0);
                highlightItem(prev);
                break;
            }
            case 'Enter': {
                if (selectedIdx >= 0 && items[selectedIdx]) {
                    items[selectedIdx].click();
                }
                break;
            }
            case 'Escape': {
                input.value = '';
                clearResults();
                break;
            }
        }
    }

    function handleFocus(): void {
        ensureIndex();
    }

    function handleBlur(): void {
        // Why: delay allows click on result to fire first
        setTimeout(clearResults, 150);
    }

    input.addEventListener('input', handleInput);
    input.addEventListener('keydown', handleKeydown);
    input.addEventListener('focus', handleFocus);
    input.addEventListener('blur', handleBlur);

    return {
        destroy(): void {
            clearTimeout(debounceTimer);
            input.removeEventListener('input', handleInput);
            input.removeEventListener('keydown', handleKeydown);
            input.removeEventListener('focus', handleFocus);
            input.removeEventListener('blur', handleBlur);
            container.innerHTML = '';
        },
    };
}
