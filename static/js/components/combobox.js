/**
 * Combobox (Autocomplete) Component
 * Encapsulates the logic for a searchable, debounced dropdown.
 * BEM: .c-combobox
 */

class Combobox {
    constructor(container, options = {}) {
        this.container = container;
        this.options = {
            placeholder: options.placeholder || 'Search...',
            searchFn: options.searchFn || (async () => []),
            onSelect: options.onSelect || (() => {}),
            debounceMs: options.debounceMs || 250,
            renderItem: options.renderItem || this.defaultRenderItem,
            id: options.id || `combobox-${Math.random().toString(36).substr(2, 9)}`
        };

        this.input = null;
        this.menu = null;
        this.items = [];
        this.activeIndex = -1;
        this.debounceTimer = null;

        this.init();
    }

    init() {
        this.container.innerHTML = `
            <div class="c-combobox" id="${this.options.id}">
                <input type="text" class="c-combobox__input" placeholder="${this.options.placeholder}" autocomplete="off">
                <div class="c-combobox__menu"></div>
            </div>
        `;

        this.input = this.container.querySelector('.c-combobox__input');
        this.menu = this.container.querySelector('.c-combobox__menu');

        this.setupEvents();
    }

    setupEvents() {
        this.input.addEventListener('input', () => {
            clearTimeout(this.debounceTimer);
            this.debounceTimer = setTimeout(() => this.handleSearch(), this.options.debounceMs);
        });

        this.input.addEventListener('keydown', (e) => this.handleKeydown(e));

        // Close menu when clicking outside
        document.addEventListener('click', (e) => {
            if (!this.container.contains(e.target)) {
                this.hideMenu();
            }
        });

        // Show menu if input has value on focus
        this.input.addEventListener('focus', () => {
            if (this.items.length > 0) {
                this.showMenu();
            }
        });
    }

    async handleSearch() {
        const query = this.input.value.trim();
        if (!query) {
            this.hideMenu();
            return;
        }

        try {
            this.items = await this.options.searchFn(query);
            this.renderMenu();
            if (this.items.length > 0) {
                this.showMenu();
            } else {
                this.renderNoResults();
                this.showMenu();
            }
        } catch (err) {
            console.error('[Combobox] Search failed:', err);
        }
    }

    renderMenu() {
        this.menu.innerHTML = '';
        this.activeIndex = -1;

        this.items.forEach((item, index) => {
            const itemEl = document.createElement('div');
            itemEl.className = 'c-combobox__item';
            itemEl.innerHTML = this.options.renderItem(item);
            itemEl.addEventListener('click', () => this.selectItem(index));
            this.menu.appendChild(itemEl);
        });
    }

    renderNoResults() {
        this.menu.innerHTML = '<div class="c-combobox__no-results">No contacts found</div>';
    }

    handleKeydown(e) {
        if (!this.menu.classList.contains('c-combobox__menu--visible')) return;

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
                break;
        }
    }

    moveActive(delta) {
        const itemEls = this.menu.querySelectorAll('.c-combobox__item');
        if (itemEls.length === 0) return;

        if (this.activeIndex >= 0) {
            itemEls[this.activeIndex].classList.remove('c-combobox__item--active');
        }

        this.activeIndex = (this.activeIndex + delta + itemEls.length) % itemEls.length;
        itemEls[this.activeIndex].classList.add('c-combobox__item--active');
        itemEls[this.activeIndex].scrollIntoView({ block: 'nearest' });
    }

    selectItem(index) {
        const item = this.items[index];
        this.options.onSelect(item);
        this.clear();
        this.hideMenu();
    }

    clear() {
        this.input.value = '';
        this.items = [];
    }

    showMenu() {
        this.menu.classList.add('c-combobox__menu--visible');
    }

    hideMenu() {
        this.menu.classList.remove('c-combobox__menu--visible');
    }

    defaultRenderItem(item) {
        return `
            <div class="c-combobox__item-title">${item.display_name || item.canonical_id}</div>
            <div class="c-combobox__item-subtitle">${item.canonical_id}</div>
        `;
    }
}

window.Combobox = Combobox;
