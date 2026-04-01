const fs = require('fs');
const path = require('path');

const INDEX_HTML_PATH = path.join(__dirname, 'index.html');

function verifyMainIDs() {
    console.log('--- Verifying main HTML IDs ---');
    const content = fs.readFileSync(INDEX_HTML_PATH, 'utf8');
    const requiredIDs = [
        'dashViewBtn', 'archiveViewBtn', 'insightsViewBtn', 'settingsBtn',
        'archiveSection', 'archiveSearchInput', 'loading'
    ];
    let allPassed = true;
    requiredIDs.forEach(id => {
        if (!content.includes(`id="${id}"`)) {
            console.error(`❌ ID "${id}" MISSING in index.html`);
            allPassed = false;
        }
    });
    if (allPassed) console.log('✅ All required IDs found in index.html.');
    return allPassed;
}

function verifyNoHardcodedStyles() {
    console.log('--- Verifying no hardcoded styles in HTML ---');
    const content = fs.readFileSync(INDEX_HTML_PATH, 'utf8');
    // Check for style="..." attributes except for display: block/none or common positioning
    const styleAttrRegex = / style="([^"]*)"/g;
    let match;
    let allPassed = true;
    while ((match = styleAttrRegex.exec(content)) !== null) {
        const styleContent = match[1].toLowerCase();
        // Allow display, but forbid border-radius, color, background-color, hex codes, or rgb
        if (styleContent.includes('color:') || styleContent.includes('#') || styleContent.includes('rgb(') || 
            styleContent.includes('border-radius:') || styleContent.includes('filter: blur')) {
            console.error(`❌ Hardcoded style detected: style="${match[1]}"`);
            allPassed = false;
        }
    }
    if (allPassed) console.log('✅ No hardcoded styles detected in index.html.');
    return allPassed;
}

function verifyInitialState() {
    console.log('--- Verifying initial UI state ---');
    const content = fs.readFileSync(INDEX_HTML_PATH, 'utf8');
    // Loading overlay should exist but NOT have "active" class in the source
    const loadingBlockRegex = /id="loading"[^>]*class="([^"]*)"/;
    const match = content.match(loadingBlockRegex);
    if (match) {
        const classes = match[1].split(' ');
        if (classes.includes('active')) {
            console.error('❌ Loading overlay is ACTIVE by default (source level).');
            return false;
        }
    }
    console.log('✅ Initial state verified.');
    return true;
}

const idsOk = verifyMainIDs();
const stylesOk = verifyNoHardcodedStyles();
const stateOk = verifyInitialState();

if (idsOk && stylesOk && stateOk) {
    console.log('\n✨ HTML Static Verification PASSED ✨');
    process.exit(0);
} else {
    console.error('\n❌ HTML Static Verification FAILED');
    process.exit(1);
}
