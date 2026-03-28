const fs = require('fs');
const path = require('path');

const STATIC_CSS_DIR = path.join(__dirname, 'static/css');
const COMPONENTS_DIR = path.join(STATIC_CSS_DIR, 'components');
const RENDERER_JS_PATH = path.join(__dirname, 'static/js/renderer.js');

const HARDCODED_VALUES_REGEX = /(?<!var\(--)[0-9]+px|#[0-9a-fA-F]{3,6}/g;
const BEM_CLASS_REGEX = /c-[a-zA-Z0-9\-_]*/g; // General BEM match

function getStyleFiles() {
    let files = [];
    
    // Core files (excluding variables.css as it contains definitions)
    const coreFiles = ['base.css', 'layout.css', 'v2-nav.css', 'v2-insights.css', 'v2-modals.css', 'v2-settings.css', 'badges.css'];
    files = coreFiles.map(f => path.join(STATIC_CSS_DIR, f)).filter(f => fs.existsSync(f));
    
    // Component files
    if (fs.existsSync(COMPONENTS_DIR)) {
        const compFiles = fs.readdirSync(COMPONENTS_DIR).filter(f => f.endsWith('.css'));
        files = files.concat(compFiles.map(f => path.join(COMPONENTS_DIR, f)));
    }
    
    return files;
}

function verifyHardcodedValues() {
    console.log('--- Verifying Hardcoded Values (px, hex) ---');
    const files = getStyleFiles();
    let allPassed = true;
    
    files.forEach(file => {
        const content = fs.readFileSync(file, 'utf8');
        // Remove comments before verification
        const cleanContent = content.replace(/\/\*[\s\S]*?\*\/|\/\/.*/g, '');
        const lines = cleanContent.split('\n');
        
        let filePassed = true;
        let violations = [];

        lines.forEach((line, index) => {
            if (line.trim().startsWith('@media')) return;
            
            const hardcoded = line.match(HARDCODED_VALUES_REGEX);
            if (hardcoded) {
                violations.push(`${index + 1}: ${hardcoded.join(', ')}`);
                filePassed = false;
                allPassed = false;
            }
        });

        if (!filePassed) {
            console.error(`❌ Found hardcoded values in ${path.relative(__dirname, file)}:`);
            console.error(violations.join('\n'));
        }
    });
    
    if (allPassed) console.log('✅ No hardcoded values found in property definitions.');
    return allPassed;
}

function verifyRendererBEM() {
    console.log('--- Verifying renderer.js BEM Classes ---');
    if (!fs.existsSync(RENDERER_JS_PATH)) {
        console.warn('⚠️ renderer.js not found, skipping BEM check.');
        return true;
    }
    
    const jsContent = fs.readFileSync(RENDERER_JS_PATH, 'utf8');
    const files = getStyleFiles();
    
    let combinedCssContent = '';
    files.forEach(file => {
        combinedCssContent += fs.readFileSync(file, 'utf8') + '\n';
    });
    
    const jsClasses = new Set(jsContent.match(BEM_CLASS_REGEX) || []);
    const cssClasses = new Set(combinedCssContent.match(BEM_CLASS_REGEX) || []);
    
    let allPassed = true;
    
    for (const cls of jsClasses) {
        // Only check classes that look like components (c-)
        if (cls.startsWith('c-') && !cssClasses.has(cls)) {
            console.error(`❌ Class "${cls}" found in renderer.js but MISSING in CSS files.`);
            allPassed = false;
        }
    }
    
    if (allPassed) {
        console.log('✅ All monitored BEM classes in renderer.js are correctly defined in CSS.');
    }
    
    return allPassed;
}

function verifySemanticVariables() {
    console.log('--- Verifying Semantic RGB Variables ---');
    const variablesPath = path.join(STATIC_CSS_DIR, 'variables.css');
    if (!fs.existsSync(variablesPath)) {
        console.error('❌ variables.css MISSING');
        return false;
    }
    const content = fs.readFileSync(variablesPath, 'utf8');
    const required = [
        '--color-primary-rgb',
        '--color-success-rgb',
        '--color-warning-rgb',
        '--color-error-rgb'
    ];
    let allPassed = true;
    required.forEach(v => {
        if (!content.includes(v)) {
            console.error(`❌ Variable "${v}" MISSING in variables.css`);
            allPassed = false;
        }
    });
    if (allPassed) console.log('✅ All semantic RGB variables are present.');
    return allPassed;
}

function verifyComponentExistence() {
    console.log('--- Verifying Component/Utility Class Existence ---');
    const badgeFile = path.join(COMPONENTS_DIR, 'badges.css');
    const utilFile = path.join(COMPONENTS_DIR, 'utilities.css');
    
    let allPassed = true;
    if (fs.existsSync(badgeFile)) {
        const content = fs.readFileSync(badgeFile, 'utf8');
        if (!content.includes('.c-badge')) {
            console.error('❌ .c-badge class MISSING in badges.css');
            allPassed = false;
        }
    }
    if (fs.existsSync(utilFile)) {
        const content = fs.readFileSync(utilFile, 'utf8');
        if (!content.includes('.u-')) {
            console.error('❌ .u- utility classes MISSING in utilities.css');
            allPassed = false;
        }
    }
    if (allPassed) console.log('✅ Component/Utility classes are correctly defined.');
    return allPassed;
}

const hardcodedOk = verifyHardcodedValues();
const bemOk = verifyRendererBEM();
const semanticOk = verifySemanticVariables();
const componentOk = verifyComponentExistence();

if (hardcodedOk && bemOk && semanticOk && componentOk) {
    console.log('\n✨ Automated Verification PASSED ✨');
    process.exit(0);
} else {
    console.error('\n❌ Automated Verification FAILED');
    process.exit(1);
}
