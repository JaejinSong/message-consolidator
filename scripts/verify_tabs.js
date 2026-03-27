/**
 * verify_tabs.js
 * Logic verification for setupTabs utility.
 */

import { setupTabs } from '../static/js/utils.js';

// --- Minimal DOM Mock ---
global.document = {
    querySelectorAll: (selector) => {
        if (selector.includes('tab-btn')) return mockTabs;
        if (selector.includes('c-tabs__panel')) return mockPanels;
        return [];
    }
};

class MockElement {
    constructor(id, className) {
        this.id = id;
        this.classList = {
            classes: new Set(className.split(' ')),
            add(c) { this.classes.add(c); },
            remove(c) { this.classes.delete(c); },
            toggle(c, force) {
                if (force) this.classes.add(c);
                else this.classes.delete(c);
            },
            contains(c) { return this.classes.has(c); },
            get [0]() { return Array.from(this.classes)[0]; }
        };
        this.attributes = {};
        this.listeners = {};
    }
    setAttribute(key, val) { this.attributes[key] = val; }
    getAttribute(key) { return this.attributes[key]; }
    addEventListener(event, callback) {
        this.listeners[event] = callback;
    }
    click() {
        if (this.listeners['click']) this.listeners['click']();
    }
}

const mockTabs = [
    new MockElement('btn1', 'tab-btn active'),
    new MockElement('btn2', 'tab-btn')
];
mockTabs[0].setAttribute('data-tab', 'panel1');
mockTabs[1].setAttribute('data-tab', 'panel2');

const mockPanels = [
    new MockElement('panel1', 'c-tabs__panel c-tabs__panel--active'),
    new MockElement('panel2', 'c-tabs__panel')
];

// --- Test Execution ---
console.log('Testing setupTabs with BEM modifiers...');

setupTabs('.tab-btn', '.c-tabs__panel', 'data-tab', 'active');

console.log('Initial state:');
console.log('Tab 1 active:', mockTabs[0].classList.contains('active'));
console.log('Panel 1 active modifier:', mockPanels[0].classList.contains('c-tabs__panel--active'));

console.log('\nClicking Tab 2...');
mockTabs[1].click();

const success = !mockTabs[0].classList.contains('active') && 
                mockTabs[1].classList.contains('active') &&
                !mockPanels[0].classList.contains('c-tabs__panel--active') &&
                mockPanels[1].classList.contains('c-tabs__panel--active');

if (success) {
    console.log('✅ Success: Tab switching and BEM modifier logic verified.');
    process.exit(0);
} else {
    console.error('❌ Failure: Tab switching or BEM modifier logic failed.');
    console.log('Tab 1 active:', mockTabs[0].classList.contains('active'));
    console.log('Tab 2 active:', mockTabs[1].classList.contains('active'));
    console.log('Panel 1 modifier:', mockPanels[0].classList.contains('c-tabs__panel--active'));
    console.log('Panel 2 modifier:', mockPanels[1].classList.contains('c-tabs__panel--active'));
    process.exit(1);
}
