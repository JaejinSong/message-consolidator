const fs = require('fs');
const path = require('path');

const INDEX_PATH = path.join(__dirname, 'index.html');
const CSS_PATH = path.join(__dirname, 'static/css/layout.css');

let hasErrors = false;

function assert(condition, message) {
    if (!condition) {
        console.error(`\x1b[31m[FAIL]\x1b[0m ${message}`);
        hasErrors = true;
    } else {
        console.log(`\x1b[32m[PASS]\x1b[0m ${message}`);
    }
}

// 1. Verify index.html
const indexContent = fs.readFileSync(INDEX_PATH, 'utf8');
assert(
    indexContent.includes('id="archiveSection" class="hidden glass-card">') && 
    !indexContent.includes('style="margin-top: 0.5rem; padding: 1.25rem 1.5rem;'),
    '#archiveSection should not have inline layout styles and should have glass-card class.'
);

// 2. Verify layout.css definition
const cssContent = fs.readFileSync(CSS_PATH, 'utf8');
assert(
    cssContent.includes('.glass-card {') && 
    cssContent.includes('margin-top: var(--view-subnav-gap);') &&
    cssContent.includes('padding: var(--spacing-xl) var(--spacing-2xl);'),
    '.glass-card should be defined in layout.css with desktop spacing tokens.'
);

// 3. Verify mobile override
assert(
    cssContent.includes('@media (max-width: 768px)') &&
    cssContent.match(/\.glass-card\s*\{\s*margin-top:\s*0;\s*padding:\s*var\(--spacing-md\)\s*var\(--spacing-sm\);/),
    '.glass-card should have mobile overrides (margin-top: 0 and reduced padding) in 768px media query.'
);

if (hasErrors) {
    process.exit(1);
} else {
    console.log('\n\x1b[32mAll layout consistency checks passed!\x1b[0m');
    process.exit(0);
}
