import { AccountItem, ComboboxOptions, ComboboxInterface } from '../types';

/**
 * Combobox (Autocomplete) Component
 * TypeScript implementation with proper lifecycle management.
 * BEM: .c-combobox
 */
export class Combobox implements ComboboxInterface {
    private container: HTMLElement;
    private options: Required<ComboboxOptions>;
    private input: HTMLInputElement | null = null;
    private menu: HTMLElement | null = null;
    private items: AccountItem[] = [];
    private activeIndex: number = -1;
    private selectedItem: AccountItem | null = null;
    private debounceTimer: ReturnType<typeof setTimeout> | null = null;

    // Bound event handlers for robust removal
    private onInputHandler = () => this.onInput();
    private onKeyDownHandler = (e: KeyboardEvent) => this.handleKeydown(e);
    private onFocusHandler = () => this.onFocus();
    private onDocumentClickHandler = (e: MouseEvent) => this.handleDocumentClick(e);

    constructor(container: HTMLElement, options: ComboboxOptions) {
        this.container = container;
        this.options = {
            placeholder: options.placeholder || 'Search...',
            searchFn: options.searchFn,
            onSelect: options.onSelect || (() => {}),
            debounceMs: options.debounceMs || 250,
            renderItem: options.renderItem || this.defaultRenderItem,
            id: options.id || `combobox-${Math.random().toString(36).substring(2, 11)}`
        };

        this.render();
        this.setupEvents();
    }

    /**
     * Renders the initial component structure.
     */
    private render(): void {
        this.container.innerHTML = `
            <div class="c-combobox" id="${this.options.id}">
                <input type="text" class="c-combobox__input" 
                       placeholder="${this.options.placeholder}" 
                       autocomplete="off" 
                       aria-autocomplete="list">
                <div class="c-combobox__menu" role="listbox"></div>
            </div>
        `;

        this.input = this.container.querySelector<HTMLInputElement>('.c-combobox__input');
        this.menu = this.container.querySelector<HTMLElement>('.c-combobox__menu');

        if (!this.input || !this.menu) {
            throw new Error('[Combobox] Failed to initialize DOM elements');
        }
    }

    /**
     * Attaches all necessary event listeners.
     */
    private setupEvents(): void {
        if (!this.input) return;

        this.input.addEventListener('input', this.onInputHandler);
        this.input.addEventListener('keydown', this.onKeyDownHandler);
        this.input.addEventListener('focus', this.onFocusHandler);
        document.addEventListener('click', this.onDocumentClickHandler);
    }

    /**
     * Logic for input changes with debouncing.
     */
    private onInput(): void {
        if (this.debounceTimer) clearTimeout(this.debounceTimer);
        this.debounceTimer = setTimeout(() => this.handleSearch(), this.options.debounceMs);
    }

    /**
     * Logic for focusing the input.
     */
    private onFocus(): void {
        if (this.items.length > 0) {
            this.showMenu();
        }
    }

    /**
     * Closes menu when clicking outside the component.
     */
    private handleDocumentClick(e: MouseEvent): void {
        const target = e.target as HTMLElement;
        if (!this.container.contains(target)) {
            this.hideMenu();
        }
    }

    /**
     * Performs the search and updates the menu.
     */
    private async handleSearch(): Promise<void> {
        if (!this.input) return;

        const query = this.input.value.trim();
        if (!query) {
            this.items = [];
            this.hideMenu();
            return;
        }

        try {
            this.items = await this.options.searchFn(query);
            this.renderMenu();
            this.showMenu();
        } catch (err) {
            console.error('[Combobox] Search failed:', err);
            this.hideMenu();
        }
    }

    /**
     * Renders matching items into the dropdown menu.
     */
    private renderMenu(): void {
        if (!this.menu) return;

        this.menu.innerHTML = '';
        this.activeIndex = -1;

        if (this.items.length === 0) {
            const noResults = document.createElement('div');
            noResults.className = 'c-combobox__no-results';
            noResults.textContent = 'No results found';
            this.menu.appendChild(noResults);
            return;
        }

        this.items.forEach((item, index) => {
            const itemEl = document.createElement('div');
            itemEl.className = 'c-combobox__item';
            itemEl.setAttribute('role', 'option');
            itemEl.innerHTML = this.options.renderItem(item);
            itemEl.addEventListener('click', () => this.selectItem(index));
            this.menu?.appendChild(itemEl);
        });
    }

    /**
     * Keyboard navigation support (Arrow keys, Enter, Escape).
     */
    private handleKeydown(e: KeyboardEvent): void {
        if (!this.menu?.classList.contains('c-combobox__menu--visible')) return;

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                this.moveActive(1);
                break;
            case 'ArrowUp':
                e.preventDefault();
                this.moveActive(-1);
                break;
            case 'Enter':
                e.preventDefault();
                if (this.activeIndex >= 0) {
                    this.selectItem(this.activeIndex);
                }
                break;
            case 'Escape':
                this.hideMenu();
                this.input?.blur();
                break;
        }
    }

    /**
     * Moves the highlight in the dropdown menu.
     */
    private moveActive(delta: number): void {
        if (!this.menu) return;

        const itemEls = this.menu.querySelectorAll<HTMLElement>('.c-combobox__item');
        if (itemEls.length === 0) return;

        // Guard: remove existing active class
        if (this.activeIndex >= 0 && itemEls[this.activeIndex]) {
            itemEls[this.activeIndex].classList.remove('c-combobox__item--active');
        }

        // Calculate new index with wrap-around
        this.activeIndex = (this.activeIndex + delta + itemEls.length) % itemEls.length;
        
        const activeItem = itemEls[this.activeIndex];
        activeItem.classList.add('c-combobox__item--active');
        activeItem.scrollIntoView({ block: 'nearest' });
    }

    /**
     * Selects an item from the list.
     */
    private selectItem(index: number): void {
        const item = this.items[index];
        if (!item) return;

        this.selectedItem = item;
        if (this.input) {
            this.input.value = item.display_name || item.canonical_id;
        }
        
        this.options.onSelect(item);
        this.hideMenu();
    }

    /**
     * Public API: Returns the currently selected item.
     */
    public getValue(): AccountItem | null {
        return this.selectedItem;
    }

    /**
     * Public API: Clears the state and input.
     */
    public clear(): void {
        this.selectedItem = null;
        this.items = [];
        if (this.input) this.input.value = '';
        this.hideMenu();
    }

    /**
     * Lifecycle: Cleans up all resources and event listeners.
     */
    public destroy(): void {
        if (this.debounceTimer) clearTimeout(this.debounceTimer);
        
        if (this.input) {
            this.input.removeEventListener('input', this.onInputHandler);
            this.input.removeEventListener('keydown', this.onKeyDownHandler);
            this.input.removeEventListener('focus', this.onFocusHandler);
        }
        
        document.removeEventListener('click', this.onDocumentClickHandler);
        
        this.container.innerHTML = '';
        this.items = [];
        this.selectedItem = null;
    }

    private showMenu(): void {
        this.menu?.classList.add('c-combobox__menu--visible');
    }

    private hideMenu(): void {
        this.menu?.classList.remove('c-combobox__menu--visible');
    }

    private defaultRenderItem(item: AccountItem): string {
        const title = item.display_name || item.canonical_id;
        const subtitle = item.canonical_id;
        return `
            <div class="c-combobox__item-title">${title}</div>
            <div class="c-combobox__item-subtitle">${subtitle}</div>
        `;
    }
}
