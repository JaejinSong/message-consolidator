/**
 * @file translation_logic.test.ts
 * @description Vitest suite for English-First fallback strategy validation.
 */

import { describe, it, expect } from 'vitest';
import { getDisplayTask } from '../logic.ts';
import { Message } from '../types.ts';

const mockMessages: Partial<Message>[] = [
    { id: 1, task: "Original Task", task_en: "English Task", task_ko: "한국어 태스크" },
    { id: 2, task: "Original Task", task_en: "English Task Only", task_ko: null } as any,
    { id: 3, task: "Legacy Task", task_en: null, task_ko: null } as any,
    { id: 4, task_en: "Source Task", task_ko: "번역됨" }
];

describe('Translation Logic (English-First Fallback)', () => {
    it('should prefer Korean when requested and available', () => {
        const result = getDisplayTask(mockMessages[0] as Message, 'ko');
        expect(result).toBe("한국어 태스크");
    });

    it('should fallback to English if Korean is requested but missing', () => {
        const result = getDisplayTask(mockMessages[1] as Message, 'ko');
        expect(result).toBe("English Task Only");
    });

    it('should fallback to the legacy task field if translations are missing', () => {
        const result = getDisplayTask(mockMessages[2] as Message, 'ko');
        expect(result).toBe("Legacy Task");
    });

    it('should use the default language (ko) when no language is specified', () => {
        const result = getDisplayTask(mockMessages[3] as Message);
        expect(result).toBe("번역됨");
    });
});
