const fs = require('fs');
const path = require('path');

const V2_CSS_PATH = path.join(__dirname, 'static/css/v2-components.css');
const RENDERER_JS_PATH = path.join(__dirname, 'static/js/renderer.js');

const HARDCODED_VALUES_REGEX = /(?<!var\(--)[0-9]+px|#[0-9a-fA-F]{3,6}/g;
const BEM_CLASS_REGEX = /c-task-card[a-zA-Z0-9\-_]*/g;

function verifyV2Components() {
    console.log('--- Verifying v2-components.css ---');
    const cssContent = fs.readFileSync(V2_CSS_PATH, 'utf8');
    
    // Filter out @media lines which cannot use CSS variables in standard CSS
    const lines = cssContent.split('\n');
    const propertyLines = lines.filter(line => !line.trim().startsWith('@media'));
    const contentToVerify = propertyLines.join('\n');
    
    const hardcoded = contentToVerify.match(HARDCODED_VALUES_REGEX);
    if (hardcoded && hardcoded.length > 0) {
        console.error('❌ Found hardcoded values in v2-components.css:');
        console.error(Array.from(new Set(hardcoded)).join(', '));
        return false;
    }
    
    console.log('✅ No hardcoded values (px, hex) found in property definitions.');
    return true;
}

function verifyRendererBEM() {
    console.log('--- Verifying renderer.js BEM Classes ---');
    const jsContent = fs.readFileSync(RENDERER_JS_PATH, 'utf8');
    const cssContent = fs.readFileSync(V2_CSS_PATH, 'utf8');
    
    const jsClasses = new Set(jsContent.match(BEM_CLASS_REGEX) || []);
    const cssClasses = new Set(cssContent.match(BEM_CLASS_REGEX) || []);
    
    let allPassed = true;
    
    for (const cls of jsClasses) {
        if (!cssClasses.has(cls)) {
            // Special exception for logical modifiers if applied dynamically (none yet in our case)
            console.error(`❌ Class "${cls}" found in renderer.js but MISSING in v2-components.css`);
            allPassed = false;
        }
    }
    
    if (allPassed) {
        console.log('✅ All BEM classes in renderer.js are correctly defined in CSS.');
    }
    
    return allPassed;
}

const v2Ok = verifyV2Components();
const rendererOk = verifyRendererBEM();

if (v2Ok && rendererOk) {
    console.log('\n✨ Level 4 Automated Verification PASSED ✨');
    process.exit(0);
} else {
    console.error('\n❌ Level 4 Automated Verification FAILED');
    process.exit(1);
}
