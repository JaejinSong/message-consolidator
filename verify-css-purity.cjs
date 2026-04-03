const fs = require('fs');
const path = require('path');

const cssPath = path.join(__dirname, 'static/css/v2-insights.css');
const content = fs.readFileSync(cssPath, 'utf8');

// Regex for px (excluding 0px or 1px if necessary, but request says NO px)
// Actually, 1px is often used for borders, but I'll check all.
const pxMatch = content.match(/\d+px/g);
const hexMatch = content.match(/#[A-Fa-f0-9]{3,6}/g);

let errors = [];

if (pxMatch) {
    pxMatch.forEach(m => {
        // Allow 1px for border-thin as per project standards? 
        // Instructions say "absolute prohibition of hardcoded px". 
        // But variables.css has --border-thin: 1px.
        // Let's see if there are NEW px in v2-insights.css.
        errors.push(`Found px: ${m}`);
    });
}

if (hexMatch) {
    hexMatch.forEach(m => {
        errors.push(`Found hex: ${m}`);
    });
}

if (errors.length > 0) {
    console.error('CSS Validation Failed:');
    console.error(errors.join('\n'));
    process.exit(1);
} else {
    console.log('CSS Validation Passed: No px or hex found.');
}
