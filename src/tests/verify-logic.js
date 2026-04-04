/**
 * @file verify-logic.js
 * @description Pure JS verification of the getDisplayTask logic.
 */

// Mimic the function from logic.ts
function getDisplayTask(m, lang) {
    const targetLang = lang || 'ko';
    
    // 1. If Korean requested and exists, use it
    if (targetLang === 'ko' && m.task_ko) {
        return m.task_ko;
    }
    
    // 2. Fallback to English 원문 (task_en or task)
    return m.task_en || m.task || "";
}

const mockMessages = [
    { id: 1, task: "Original Task", task_en: "English Task", task_ko: "한국어 태스크" },
    { id: 2, task: "Original Task", task_en: "English Task Only", task_ko: null },
    { id: 3, task: "Legacy Task", task_en: null, task_ko: null },
    { id: 4, task_en: "Source Task", task_ko: "번역됨" }
];

function runTests() {
    console.log("Running Pure JS Translation Logic Tests...");

    const tests = [
        { desc: "Korean Fallback", m: mockMessages[0], lang: 'ko', expected: "한국어 태스크" },
        { desc: "English Fallback (Missing KO)", m: mockMessages[1], lang: 'ko', expected: "English Task Only" },
        { desc: "Legacy Fallback (.task only)", m: mockMessages[2], lang: 'ko', expected: "Legacy Task" },
        { desc: "Default Language Fallback", m: mockMessages[3], lang: undefined, expected: "번역됨" }
    ];

    tests.forEach((t, i) => {
        const result = getDisplayTask(t.m, t.lang);
        if (result !== t.expected) {
            throw new Error(`Test ${i+1} (${t.desc}) Failed: Expected "${t.expected}", got "${result}"`);
        }
        console.log(`✅ Test ${i+1} Passed: ${t.desc}`);
    });

    console.log("\nAll logic tests passed successfully!");
}

runTests();
