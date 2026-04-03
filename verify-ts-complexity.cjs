const fs = require('fs');
const path = require('path');

const tsPath = path.join(__dirname, 'src/insightsRenderer.ts');
const lines = fs.readFileSync(tsPath, 'utf8').split('\n');

let functionName = '';
let startLine = 0;
let braceCount = 0;
let violations = [];

lines.forEach((line, index) => {
    // Basic match for function declarations or object methods
    const match = line.match(/(?:(?:public|private|static)\s*)?([a-zA-Z0-9]+)\s*\([^)]*\)\s*.*\{/);
    if (match && braceCount === 0) {
        functionName = match[1];
        startLine = index + 1;
        braceCount = 0;
    }

    if (line.includes('{')) braceCount += (line.match(/\{/g) || []).length;
    if (line.includes('}')) braceCount -= (line.match(/\}/g) || []).length;

    if (braceCount === 0 && functionName) {
        const length = index + 1 - startLine;
        if (length > 30) {
            violations.push(`${functionName} (Lines ${startLine}-${index + 1}): ${length} lines`);
        }
        functionName = '';
    }
});

if (violations.length > 0) {
    console.error('TS Complexity Check Failed: Functions longer than 30 lines found:');
    console.error(violations.join('\n'));
    process.exit(1);
} else {
    console.log('TS Complexity Check Passed: All functions are under 30 lines.');
}
