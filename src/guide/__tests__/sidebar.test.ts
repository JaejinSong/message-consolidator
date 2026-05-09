// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { mountSidebar } from '../sidebar';
import type { SidebarController } from '../sidebar';
import { GUIDE_SECTIONS } from '../content';
import type { GuideSection } from '../content';

describe('mountSidebar', () => {
    let container: HTMLElement;
    let onSelect: Mock<(section: GuideSection) => void>;
    let controller: SidebarController;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        onSelect = vi.fn<(section: GuideSection) => void>();
        controller = mountSidebar(container, GUIDE_SECTIONS, onSelect);
    });

    it('renders one button per section', () => {
        const buttons = container.querySelectorAll('button');
        expect(buttons.length).toBe(GUIDE_SECTIONS.length);
    });

    it('each button has correct role, aria-controls, data-section, and BEM class', () => {
        GUIDE_SECTIONS.forEach(s => {
            const btn = container.querySelector<HTMLButtonElement>(`#guide-tab-${s.key}`);
            expect(btn).not.toBeNull();
            expect(btn!.getAttribute('role')).toBe('tab');
            expect(btn!.getAttribute('aria-controls')).toBe('guideContent');
            expect(btn!.getAttribute('data-section')).toBe(s.key);
            expect(btn!.classList.contains('c-guide__sidebar-btn')).toBe(true);
        });
    });

    it('container receives role=tablist and aria-orientation=vertical', () => {
        expect(container.getAttribute('role')).toBe('tablist');
        expect(container.getAttribute('aria-orientation')).toBe('vertical');
    });

    it('first button starts with tabindex=0 (roving tabindex initial state)', () => {
        const first = container.querySelector<HTMLButtonElement>('button:first-child')!;
        // Why: aria-selected starts false for all; tabindex=0 marks roving focus entry point
        expect(first.tabIndex).toBe(0);
    });

    it('all buttons start with aria-selected=false', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons.forEach(btn => {
            expect(btn.getAttribute('aria-selected')).toBe('false');
        });
    });

    it('subsequent buttons start with tabindex=-1', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons.slice(1).forEach(btn => {
            expect(btn.tabIndex).toBe(-1);
        });
    });

    it('click on a button calls onSelect with correct section key', () => {
        const secondKey = GUIDE_SECTIONS[1].key;
        const btn = container.querySelector<HTMLButtonElement>(`#guide-tab-${secondKey}`)!;
        btn.click();
        expect(onSelect).toHaveBeenCalledOnce();
        expect(onSelect).toHaveBeenCalledWith(secondKey);
    });

    it('ArrowDown moves selection and focus to next button', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons[0].focus();
        buttons[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
        expect(buttons[1].getAttribute('aria-selected')).toBe('true');
        expect(buttons[1].tabIndex).toBe(0);
        expect(buttons[0].getAttribute('aria-selected')).toBe('false');
    });

    it('ArrowUp from first wraps to last', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons[0].focus();
        buttons[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
        const last = buttons[buttons.length - 1];
        expect(last.getAttribute('aria-selected')).toBe('true');
        expect(last.tabIndex).toBe(0);
    });

    it('Home jumps to first button', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons[2].focus();
        buttons[2].dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true }));
        expect(buttons[0].getAttribute('aria-selected')).toBe('true');
    });

    it('End jumps to last button', () => {
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons[0].focus();
        buttons[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
        const last = buttons[buttons.length - 1];
        expect(last.getAttribute('aria-selected')).toBe('true');
    });

    it('setActive updates aria-selected and tabindex without a click', () => {
        const targetKey = GUIDE_SECTIONS[2].key;
        controller.setActive(targetKey);
        const buttons = Array.from(container.querySelectorAll<HTMLButtonElement>('button'));
        buttons.forEach((btn, i) => {
            const expected = GUIDE_SECTIONS[i].key === targetKey;
            expect(btn.getAttribute('aria-selected')).toBe(String(expected));
            expect(btn.tabIndex).toBe(expected ? 0 : -1);
        });
    });

    it('destroy removes event listeners — click after destroy does not call onSelect', () => {
        controller.destroy();
        const btn = container.querySelector<HTMLButtonElement>('button')!;
        btn.click();
        expect(onSelect).not.toHaveBeenCalled();
    });
});
