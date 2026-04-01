/**
 * verify-render-logic.cjs
 * Logic verification script for settings-renderer.ts
 */

const fs = require('fs');
const path = require('path');

// Mock escapeHTML
const escapeHTML = (str) => {
    if (!str) return "";
    return String(str)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
};

// Mock document and container
const mockContainer = {
    innerHTML: ""
};
const mockDocument = {
    getElementById: (id) => {
        if (id === 'normList' || id === 'linkedAccountsList') return mockContainer;
        return null;
    },
    querySelectorAll: () => []
};

// Global mocks
global.document = mockDocument;
global.escapeHTML = escapeHTML;

const filePath = path.join(__dirname, 'src/renderers/settings-renderer.ts');
let content = fs.readFileSync(filePath, 'utf8');

// Strip TypeScript syntax to run in Node (cheap way)
content = content
    .replace(/import {.*} from '.*';/g, '')
    .replace(/export interface [^{]*{[^}]*}/g, '')
    .replace(/export interface [^;]*;/g, '')
    .replace(/export type [^;]*;/g, '')
    .replace(/: void/g, '')
    .replace(/: any\[\]/g, '')
    .replace(/: any/g, '')
    .replace(/: string/g, '')
    .replace(/: number/g, '')
    .replace(/: boolean/g, '')
    .replace(/: \w+\[\]/g, '')
    .replace(/: Promise<[^>]*>/g, '')
    .replace(/: \([^)]*\)\s*=>\s*[^,);]*/g, '') // Strip function types e.g. : (a) => void
    .replace(/: \w+/g, '')
    .replace(/<[^>]*>/g, '')
    .replace(/as HTMLElement/g, '')
    .replace(/export /g, '');

// Manually inject escapeHTML since we stripped imports
const testCode = `
${content}

// Test renderTenantAliasList
const testAliases = [
    { id: 101, canonical_id: "userA", display_name: "User A (Real Name)" },
    { id: "102", canonical_id: "userB", display_name: "" }
];

renderTenantAliasList(testAliases, () => {});
const output1 = mockContainer.innerHTML;
console.log("--- renderTenantAliasList Output ---");
console.log(output1);

if (!output1.includes("userA &amp;rarr; User A (Real Name)")) {
    console.error("FAIL: renderTenantAliasList did not use &rarr; format correctly for userA");
    process.exit(1);
}
if (!output1.includes('data-id="101"')) {
    console.error("FAIL: data-id for item 101 is incorrect");
    process.exit(1);
}
if (!output1.includes('data-id="102"')) {
     console.error("FAIL: data-id for item 102 should be 102 (Number conversion check)");
     process.exit(1);
}

// Test renderLinkedAccounts
const testLinks = [
    {
        target_id: 201,
        target: { canonical_id: "T1", display_name: "Target1" },
        master: { canonical_id: "M1", display_name: "Master1" }
    }
];
renderLinkedAccounts(testLinks, () => {});
const output2 = mockContainer.innerHTML;
console.log("\\n--- renderLinkedAccounts Output ---");
console.log(output2);

if (!output2.includes("Target1") || !output2.includes("Master1")) {
    console.error("FAIL: renderLinkedAccounts did not use display_name");
    process.exit(1);
}
if (!output2.includes('data-id="201"')) {
    console.error("FAIL: data-id for linked account is incorrect");
    process.exit(1);
}

console.log("\\nSUCCESS: Rendering logic verified.");
`;

eval(testCode);
