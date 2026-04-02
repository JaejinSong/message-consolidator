const fs = require('fs');
const path = require('path');

const COLORS = {
    reset: "\x1b[0m",
    red: "\x1b[31m",
    green: "\x1b[32m",
    yellow: "\x1b[33m",
    cyan: "\x1b[36m"
};

const CSS_FILE = 'static/css/v2-insights.css';
const JS_FILE = 'src/insightsRenderer.js';
const HTML_FILE = 'index.html';

console.log(`${COLORS.cyan}[BUILD] Starting Full Insights UI Verification...${COLORS.reset}\n`);

let hasError = false;

function logError(msg) {
    console.error(`${COLORS.red}[FAIL] ${msg}${COLORS.reset}`);
    hasError = true;
}

function logSuccess(msg) {
    console.log(`${COLORS.green}[PASS] ${msg}${COLORS.reset}`);
}

// 1. CSS Hardcoded Value Check
const cssContent = fs.readFileSync(CSS_FILE, 'utf8');
const hardcodedPatterns = [
    { name: 'HEX Color', regex: /#[0-9a-fA-F]{3,6}/g },
    { name: 'PX Unit', regex: /[0-9.]+px/g },
    { name: 'RGBA', regex: /rgba\([^)]+\)/g }
];

console.log(`${COLORS.yellow}[CSS] Checking hardcoded values in ${CSS_FILE}...${COLORS.reset}`);
hardcodedPatterns.forEach(p => {
    // Exception for line-height: 1 or specific 0 values
    const matches = (cssContent.match(p.regex) || []).filter(m => {
        if (m === '0px') return false;
        // Allow rgba if used in glassmorphism (though tokens are preferred)
        if (p.name === 'RGBA') return !m.includes('var(');
        return true;
    });
    if (matches.length > 0) {
        logError(`Found ${matches.length} hardcoded ${p.name}: ${matches.slice(0, 3).join(', ')}...`);
    } else {
        logSuccess(`No hardcoded ${p.name} found.`);
    }
});

// 2. BEM Matching Check (JS -> CSS)
const jsContent = fs.readFileSync(JS_FILE, 'utf8');
const BEM_REGEX = /c-[a-z0-9]+(_[a-z0-9]+)?(__[a-z0-9-]+)?(--[a-z0-9-]+)?/g;

console.log(`\n${COLORS.yellow}[BEM] Checking JS class synchronization...${COLORS.reset}`);
const jsClasses = new Set(jsContent.match(BEM_REGEX) || []);
const cssClasses = new Set(cssContent.match(BEM_REGEX) || []);

// Optional: HTML classes too
const htmlContent = fs.readFileSync(HTML_FILE, 'utf8');
const htmlClasses = new Set(htmlContent.match(BEM_REGEX) || []);

const allTargetClasses = new Set([...jsClasses, ...htmlClasses]);

// Insights 관련 접두사만 필터링 (노이즈 제거)
const INSIGHTS_PREFIXES = ['c-insights', 'c-heatmap', 'c-chart', 'c-achievement', 'c-source-dist', 'c-hourly-heatmap'];

let missingCount = 0;
allTargetClasses.forEach(cls => {
    // Prefix 필터링
    const hasValidPrefix = INSIGHTS_PREFIXES.some(prefix => cls.startsWith(prefix));
    if (!hasValidPrefix) return;

    // Skip utility prefix 'u-' and common states
    if (cls.startsWith('u-') || cls.includes('is-')) return;
    
    // Strict underscore check: must have double underscore if it has any element
    if (cls.includes('_') && !cls.includes('__')) {
        logError(`Invalid BEM naming (single underscore): ${cls}`);
        missingCount++;
    }

    if (!cssClasses.has(cls)) {
        logError(`Class '${cls}' used in JS/HTML is missing from ${CSS_FILE}`);
        missingCount++;
    }
});

if (missingCount === 0) {
    logSuccess("All BEM classes synchronized and properly named.");
}

// 3. Essential Layout Property Check
console.log(`\n${COLORS.yellow}[LAYOUT] Checking critical layout properties...${COLORS.reset}`);
const criticalLayouts = [
    { selector: '.c-heatmap__grid', props: ['grid-template-columns', 'display: grid'] },
    { selector: '.c-insights-dashboard', props: ['grid-template-columns', 'display: grid'] },
    { selector: '.c-insights-card--full', props: ['grid-column'] }
];

criticalLayouts.forEach(layout => {
    const escapedSelector = layout.selector.replace(/[-[\]{}()*+?.,\\^$|#\s]/g, '\\$&');
    const blockRegex = new RegExp(`${escapedSelector}\\s*{([^}]*)}`, 'g');
    const match = blockRegex.exec(cssContent);
    if (!match) {
        logError(`Selector '${layout.selector}' not found in CSS.`);
    } else {
        const content = match[1];
        layout.props.forEach(prop => {
            if (!content.includes(prop)) {
                logError(`'${layout.selector}' is missing essential property: ${prop}`);
            }
        });
    }
});

if (!hasError) {
    console.log(`\n${COLORS.green}✨ INSIGHTS UI VERIFICATION PASSED ✨${COLORS.reset}`);
    process.exit(0);
} else {
    console.log(`\n${COLORS.red}❌ VERIFICATION FAILED. Please fix the items above.${COLORS.reset}`);
    process.exit(1);
}
