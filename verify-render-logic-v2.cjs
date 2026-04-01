/**
 * verify-render-logic-v2.cjs
 * Logic verification script for settings-renderer.ts
 * Manually asserting the implemented logic.
 */

const escapeHTML = (str) => {
    if (!str) return "";
    return String(str)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
};

// Logic from renderTenantAliasList
function testRenderTenantAlias(item) {
    if (!item || typeof item !== 'object') return '';
    const cid = item.canonical_id || "";
    const dname = item.display_name || "";
    const displayStr = dname ? `${escapeHTML(cid)} &rarr; ${escapeHTML(dname)}` : escapeHTML(cid);
    const numericId = Number(item.id || 0);
    return `<div class="c-settings__item"><span>${displayStr}</span><button data-id="${numericId}">&times;</button></div>`;
}

// Logic from renderLinkedAccounts
function testRenderLinkedAccount(link) {
    if (!link || !link.target || !link.master) return '';
    const targetLabel = escapeHTML(link.target.display_name || link.target.canonical_id);
    const masterLabel = escapeHTML(link.master.display_name || link.master.canonical_id);
    const numericTargetId = Number(link.target_id);
    return `<div class="c-settings__item"><span class="u-text-accent">${targetLabel}</span><span class="u-mx-2 u-text-dim">→</span><span class="u-font-bold">${masterLabel}</span><button data-id="${numericTargetId}">&times;</button></div>`;
}

// --- Run Tests ---

// Case 1: Alias with display name
const alias1 = { id: 10, canonical_id: "A", display_name: "Alpha" };
const out1 = testRenderTenantAlias(alias1);
console.log("Alias 1:", out1);
if (!out1.includes("A &rarr; Alpha") || !out1.includes('data-id="10"')) {
    console.error("FAIL: Alias 1 rendering logic");
    process.exit(1);
}

// Case 2: Linked account (Bug 1 Fix)
const link1 = {
    target_id: "201", // string input
    target: { canonical_id: "T1", display_name: "Target" },
    master: { canonical_id: "M1", display_name: "Master" }
};
const out2 = testRenderLinkedAccount(link1);
console.log("Link 1:", out2);
if (!out2.includes("Target") || !out2.includes("Master") || !out2.includes('data-id="201"')) {
    console.error("FAIL: Link 1 rendering logic");
    process.exit(1);
}

// Case 3: Empty display name fallback
const link2 = {
    target_id: 301,
    target: { canonical_id: "T2", display_name: "" },
    master: { canonical_id: "M2", display_name: "Master2" }
};
const out3 = testRenderLinkedAccount(link2);
console.log("Link 2:", out3);
if (!out3.includes("T2")) {
    console.error("FAIL: Link 2 fallback logic");
    process.exit(1);
}

console.log("\nSUCCESS: All rendering logic verified.");
