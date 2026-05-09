// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { Mock } from 'vitest';
import { mountSearch } from '../search';
import { GUIDE_SECTIONS, GUIDE_CONTENT } from '../content';
import type { GuideSection } from '../content';

describe('mountSearch', () => {
    let container: HTMLElement;
    let onResultSelect: Mock<(section: GuideSection, headingId?: string) => void>;

    beforeEach(() => {
        vi.useFakeTimers();
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        onResultSelect = vi.fn<(section: GuideSection, headingId?: string) => void>();
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    function mount() {
        return mountSearch(container, GUIDE_SECTIONS, GUIDE_CONTENT, onResultSelect);
    }

    function getInput(): HTMLInputElement {
        return container.querySelector<HTMLInputElement>('input[type="search"]')!;
    }

    function getListbox(): HTMLElement {
        return container.querySelector<HTMLElement>('[role="listbox"]')!;
    }

    function triggerInput(input: HTMLInputElement, value: string): void {
        Object.defineProperty(input, 'value', { writable: true, configurable: true, value });
        input.dispatchEvent(new Event('input', { bubbles: true }));
    }

    it('renders input[type=search] and hidden listbox inside container', () => {
        mount();
        const input = getInput();
        const listbox = getListbox();
        expect(input).not.toBeNull();
        expect(listbox).not.toBeNull();
        expect(listbox.hidden).toBe(true);
    });

    it('listbox stays hidden before debounce flushes', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        expect(getListbox().hidden).toBe(true);
        vi.advanceTimersByTime(100);
        expect(getListbox().hidden).toBe(true);
    });

    it('listbox becomes visible after debounce when matches found', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);
        expect(getListbox().hidden).toBe(false);
    });

    it('case-insensitive search: "TELEGRAM" finds channels results', () => {
        mount();
        triggerInput(getInput(), 'TELEGRAM');
        vi.advanceTimersByTime(160);
        const results = getListbox().querySelectorAll('[role="option"]');
        expect(results.length).toBeGreaterThan(0);
    });

    it('empty input clears results and hides listbox', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);
        expect(getListbox().hidden).toBe(false);

        triggerInput(getInput(), '');
        vi.advanceTimersByTime(160);
        expect(getListbox().hidden).toBe(true);
    });

    it('results contain group separator with section label', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);
        const seps = getListbox().querySelectorAll('.c-guide__search-sep');
        expect(seps.length).toBeGreaterThan(0);
    });

    it('result click calls onResultSelect with section and headingId, clears input, hides listbox', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);

        const firstResult = getListbox().querySelector<HTMLElement>('[role="option"]')!;
        firstResult.click();

        expect(onResultSelect).toHaveBeenCalledOnce();
        const [section] = onResultSelect.mock.calls[0] as [string, string | undefined];
        expect(typeof section).toBe('string');
        expect(getInput().value).toBe('');
        expect(getListbox().hidden).toBe(true);
    });

    it('ArrowDown cycles through results', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);

        const input = getInput();
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));

        const items = getListbox().querySelectorAll('[role="option"]');
        expect(items[0].classList.contains('c-guide__search-result--selected')).toBe(true);
    });

    it('ArrowUp does not go below index 0', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);

        const input = getInput();
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));

        const items = getListbox().querySelectorAll('[role="option"]');
        expect(items[0].classList.contains('c-guide__search-result--selected')).toBe(true);
    });

    it('Enter activates selected result', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);

        const input = getInput();
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

        expect(onResultSelect).toHaveBeenCalledOnce();
    });

    it('Escape clears input and hides results', () => {
        mount();
        triggerInput(getInput(), 'telegram');
        vi.advanceTimersByTime(160);

        const input = getInput();
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

        expect(input.value).toBe('');
        expect(getListbox().hidden).toBe(true);
    });

    it('destroy empties container', () => {
        const ctrl = mount();
        ctrl.destroy();
        expect(container.innerHTML).toBe('');
    });

    it('no results renders hidden empty listbox', () => {
        mount();
        triggerInput(getInput(), 'zzznomatchxxx9999');
        vi.advanceTimersByTime(160);
        expect(getListbox().hidden).toBe(true);
    });
});
