import type { GuideSection } from './content';

export interface SidebarController {
    setActive(section: GuideSection): void;
    destroy(): void;
}

export function mountSidebar(
    container: HTMLElement,
    sections: { key: GuideSection; title: string; icon: string }[],
    onSelect: (section: GuideSection) => void,
): SidebarController {
    container.setAttribute('role', 'tablist');
    container.setAttribute('aria-orientation', 'vertical');

    const buttons: HTMLButtonElement[] = sections.map((s, i) => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.role = 'tab';
        btn.id = `guide-tab-${s.key}`;
        btn.setAttribute('data-section', s.key);
        btn.setAttribute('aria-selected', 'false');
        btn.setAttribute('aria-controls', 'guideContent');
        btn.tabIndex = i === 0 ? 0 : -1;
        btn.className = 'c-guide__sidebar-btn';
        btn.textContent = `${s.icon} ${s.title}`;
        return btn;
    });

    function activate(idx: number): void {
        buttons.forEach((b, i) => {
            const isActive = i === idx;
            b.setAttribute('aria-selected', String(isActive));
            b.tabIndex = isActive ? 0 : -1;
            b.classList.toggle('c-guide__sidebar-btn--active', isActive);
        });
    }

    function handleClick(e: Event): void {
        const target = e.currentTarget as HTMLButtonElement;
        const section = target.getAttribute('data-section') as GuideSection;
        const idx = buttons.indexOf(target);
        activate(idx);
        onSelect(section);
    }

    function handleKeydown(e: KeyboardEvent): void {
        const focusedIdx = buttons.findIndex(b => b === document.activeElement);
        if (focusedIdx < 0) return;

        let nextIdx = focusedIdx;

        switch (e.key) {
            case 'ArrowDown':
                nextIdx = (focusedIdx + 1) % buttons.length;
                break;
            case 'ArrowUp':
                nextIdx = (focusedIdx - 1 + buttons.length) % buttons.length;
                break;
            case 'Home':
                nextIdx = 0;
                break;
            case 'End':
                nextIdx = buttons.length - 1;
                break;
            default:
                return;
        }

        e.preventDefault();
        activate(nextIdx);
        buttons[nextIdx].focus();
        onSelect(buttons[nextIdx].getAttribute('data-section') as GuideSection);
    }

    buttons.forEach(btn => {
        btn.addEventListener('click', handleClick);
        btn.addEventListener('keydown', handleKeydown);
        container.appendChild(btn);
    });

    return {
        setActive(section: GuideSection): void {
            const idx = sections.findIndex(s => s.key === section);
            if (idx >= 0) activate(idx);
        },
        destroy(): void {
            buttons.forEach(btn => {
                btn.removeEventListener('click', handleClick);
                btn.removeEventListener('keydown', handleKeydown);
            });
        },
    };
}
