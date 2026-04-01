import { JSDOM } from 'jsdom';
import { attachCardEventListeners } from './src/renderers/task-renderer.js';

async function runTest() {
    const dom = new JSDOM('<!DOCTYPE html><div id="grid"></div>');
    const document = dom.window.document;
    global.document = document;
    global.window = dom.window;
    global.Node = dom.window.Node;
    global.HTMLElement = dom.window.HTMLElement;
    global.MouseEvent = dom.window.MouseEvent;
    global.CustomEvent = dom.window.CustomEvent;

    const grid = document.getElementById('grid');
    let capturedId = null;

    const handlers = {
        onShowOriginal: (id) => {
            capturedId = id;
        }
    };

    // Create a mock card with a string ID (like a Slack ID)
    const testId = '1671234567.000123';
    grid.innerHTML = `
        <div class="c-task-card" data-id="${testId}">
            <button class="show-original">Eye</button>
        </div>
    `;

    attachCardEventListeners(grid, handlers);

    const btn = grid.querySelector('.show-original');
    
    // Simulate click
    const clickEvent = new dom.window.MouseEvent('click', {
        bubbles: true,
        cancelable: true
    });
    btn.dispatchEvent(clickEvent);

    console.log('--- Test Result ---');
    console.log('Expected ID:', testId);
    console.log('Captured ID:', capturedId);

    if (capturedId === testId) {
        console.log('SUCCESS: ID preserved as string!');
    } else {
        console.error('FAILURE: ID mismatch or mangled!');
        process.exit(1);
    }
}

runTest().catch(err => {
    console.error(err);
    process.exit(1);
});
