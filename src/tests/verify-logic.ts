/**
 * @file verify-logic.ts
 * @description Standalone Node runner that smoke-tests getDisplayTask. Reuses the live logic
 * from logic.ts rather than duplicating it (the .js version had a copy-pasted definition).
 */

import { getDisplayTask } from '../logic';
import type { Message } from '../types';

const mockMessages = [
    { id: 1, task: 'Original Task', task_en: 'English Task', task_ko: '한국어 태스크' },
    { id: 2, task: 'Original Task', task_en: 'English Task Only', task_ko: null },
    { id: 3, task: 'Legacy Task', task_en: null, task_ko: null },
    { id: 4, task_en: 'Source Task', task_ko: '번역됨' },
] as unknown as Message[];

interface TestCase {
    desc: string;
    m: Message;
    lang: string | undefined;
    expected: string;
}

const tests: TestCase[] = [
    { desc: 'Korean Fallback',                 m: mockMessages[0], lang: 'ko',      expected: '한국어 태스크' },
    { desc: 'English Fallback (Missing KO)',   m: mockMessages[1], lang: 'ko',      expected: 'English Task Only' },
    { desc: 'Legacy Fallback (.task only)',    m: mockMessages[2], lang: 'ko',      expected: 'Legacy Task' },
    { desc: 'Default Language Fallback',       m: mockMessages[3], lang: undefined, expected: '번역됨' },
];

console.log('Running Pure TS Translation Logic Tests...');
tests.forEach((t, i) => {
    const result = getDisplayTask(t.m, t.lang);
    if (result !== t.expected) {
        throw new Error(`Test ${i + 1} (${t.desc}) Failed: Expected "${t.expected}", got "${result}"`);
    }
    console.log(`✅ Test ${i + 1} Passed: ${t.desc}`);
});
console.log('\nAll logic tests passed successfully!');
