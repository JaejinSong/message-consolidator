const postcss = require('postcss');
const safeParser = require('postcss-safe-parser');
const { globSync } = require('glob');
const fs = require('fs');
const path = require('path');

const STRICT_PROPERTIES = ['width', 'height', 'margin', 'padding', 'top', 'left', 'right', 'bottom', 'gap', 'border-radius'];
const MAX_ALLOWED_PX = 5;

let hasErrors = false;

function logError(file, line, reason) {
    console.error(`\x1b[31m[LAYOUT ERROR]\x1b[0m ${file}:${line}\n  -> ${reason}\n`);
    hasErrors = true;
}

function verifyFile(filePath) {
    const css = fs.readFileSync(filePath, 'utf8');
    const root = postcss.parse(css, { from: filePath, parser: safeParser });

    root.walkDecls(decl => {
        const { prop, value, source } = decl;
        const line = source.start.line;

        // 1. No Magic Numbers in Sizing
        if (STRICT_PROPERTIES.some(p => prop.includes(p))) {
            const pxMatch = value.match(/(\d+)px/g);
            if (pxMatch) {
                pxMatch.forEach(match => {
                    const num = parseInt(match);
                    if (num > MAX_ALLOWED_PX && !value.includes('var(')) {
                        logError(filePath, line, `Magic number detected in '${prop}: ${value}'. Use 'rem' or 'var(--spacing-*)' instead.`);
                    }
                });
            }
        }

        // 2. Restrict Legacy Layout Properties
        if (prop === 'float' && (value === 'left' || value === 'right')) {
            logError(filePath, line, `Legacy layout property 'float: ${value}' is prohibited. Use Flexbox or Grid.`);
        }
        if (prop === 'display' && value.startsWith('table')) {
            logError(filePath, line, `Legacy layout property 'display: ${value}' is prohibited.`);
        }

        // 3. Positioning Constraints (Checks for /* @layout-override */ comment)
        if (prop === 'position' && (value === 'absolute' || value === 'fixed')) {
            let hasOverride = false;
            
            // Check previous node for comment
            const prev = decl.prev();
            if (prev && prev.type === 'comment' && prev.text.includes('@layout-override')) {
                hasOverride = true;
            }
            
            // Re-check: PostCSS comments can be separate nodes or attached.
            // Some developers put it on the same line.
            if (!hasOverride) {
                // Check if the declaration itself has a comment on the same line or rule
                const parent = decl.parent;
                if (parent) {
                    parent.walkComments(comment => {
                        if (comment.source.start.line === line && comment.text.includes('@layout-override')) {
                            hasOverride = true;
                        }
                    });
                }
            }

            if (!hasOverride) {
                logError(filePath, line, `Unsanctioned '${prop}: ${value}' detected. Use a design system class or add '/* @layout-override */'.`);
            }
        }

        // 4. Z-Index Management
        if (prop === 'z-index') {
            if (!value.includes('var(--z-index-')) {
                logError(filePath, line, `Hardcoded z-index: ${value} detected. Use 'var(--z-index-*)' tokens.`);
            }
        }
    });
}

const files = globSync('static/css/**/*.css');
console.log(`\x1b[34m[LAYOUT SCAN]\x1b[0m Analyzing ${files.length} CSS files...\n`);

files.forEach(file => {
    try {
        verifyFile(file);
    } catch (e) {
        console.error(`Failed to analyze ${file}:`, e);
        hasErrors = true;
    }
});

if (hasErrors) {
    console.log('\x1b[31m[FAILED]\x1b[0m Layout Integrity check failed. Please fix the errors above.');
    process.exit(1);
} else {
    console.log('\x1b[32m[PASSED]\x1b[0m Layout Integrity check passed.');
    process.exit(0);
}
