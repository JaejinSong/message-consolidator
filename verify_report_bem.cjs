const fs = require('fs');
const path = require('path');

const insightsPath = path.join(__dirname, 'static/js/insights.js');
const rendererPath = path.join(__dirname, 'static/js/insightsRenderer.js');

const insightsContent = fs.readFileSync(insightsPath, 'utf8');
const rendererContent = fs.readFileSync(rendererPath, 'utf8');

const REQUIRED_CLASSES = [
    'c-report-item',
    'c-report-item__delete',
    'c-report-item--active'
];

const DEPRECATED_CLASSES = [
    'c-report-list__item',
    'c-report-list__item-delete',
    'is-active'
];

let errors = [];

// 1. Check for presence of required classes in both files
REQUIRED_CLASSES.forEach(cls => {
    if (!insightsContent.includes(cls)) {
        errors.push(`[ERROR] Class "${cls}" not found in insights.js`);
    }
    if (!rendererContent.includes(cls)) {
        errors.push(`[ERROR] Class "${cls}" not found in insightsRenderer.js`);
    }
});

// 2. Check for absence of deprecated classes in insights.js (relevant for the bug)
DEPRECATED_CLASSES.forEach(cls => {
    if (insightsContent.includes(`.${cls}`) || insightsContent.includes(`'${cls}'`) || insightsContent.includes(`"${cls}"`)) {
        // Special case for 'is-active' which might be used elsewhere, but for reports it should be gone from the selection logic
        if (cls === 'is-active' && insightsContent.includes('c-report-item')) {
             // Check if it's still used in the report selection part
             const reportPart = insightsContent.split('loadReportDetail')[1];
             if (reportPart && reportPart.includes('is-active')) {
                 errors.push(`[ERROR] Deprecated class "${cls}" still found in loadReportDetail in insights.js`);
             }
        } else {
            errors.push(`[ERROR] Deprecated class "${cls}" still found in insights.js`);
        }
    }
});

if (errors.length > 0) {
    console.error("BEM Consistency Check Failed:");
    errors.forEach(err => console.error(err));
    process.exit(1);
} else {
    console.log("BEM Consistency Check Passed: insights.js and insightsRenderer.js are synchronized.");
}
