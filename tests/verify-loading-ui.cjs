const fs = require('fs');
const path = require('path');

/**
 * @file verify-loading-ui.cjs
 * @description Verifies that the loading overlay and scan loading CSS rules are correctly implemented.
 * Updated for modular CSS architecture.
 */

const COMPONENTS_DIR = path.join(__dirname, '../static/css/components');
const HTML_PATH = path.join(__dirname, '../index.html');

function verify() {
    console.log('--- Loading UI Verification (Modular) ---');
    
    // 1. Check HTML for IDs
    const html = fs.readFileSync(HTML_PATH, 'utf8');
    const requiredIds = ['loading', 'archiveLoading'];
    for (const id of requiredIds) {
        if (!html.includes(`id="${id}"`)) {
            console.error(`FAIL: Missing ID "${id}" in index.html`);
            process.exit(1);
        }
    }
    console.log('✅ Required IDs found in HTML.');

    // 2. Check CSS content across components
    const spinnersCss = fs.readFileSync(path.join(COMPONENTS_DIR, 'spinners.css'), 'utf8');
    const utilitiesCss = fs.readFileSync(path.join(COMPONENTS_DIR, 'utilities.css'), 'utf8');
    
    // Check loading overlay selectors in spinners.css
    const spinnerSelectors = [
        '.loading-overlay'
    ];
    for (const sel of spinnerSelectors) {
        if (!spinnersCss.includes(sel)) {
            console.error(`FAIL: Missing CSS selector "${sel}" in spinners.css`);
            process.exit(1);
        }
    }

    // Check hidden rules in utilities.css
    if (!utilitiesCss.includes('.hidden') || !utilitiesCss.includes('display: none !important')) {
        console.error('FAIL: Missing ".hidden" rule with "!important" in utilities.css');
        process.exit(1);
    }
    
    console.log('✅ Required CSS selectors and rules found.');

    // 3. Verify specific critical properties in spinners.css
    if (!spinnersCss.includes('position: fixed') || !spinnersCss.includes('backdrop-filter')) {
        console.error('FAIL: .loading-overlay missing critical properties (fixed, blur)');
        process.exit(1);
    }

    console.log('--- Verification Successful ---');
}

verify();
