/**
 * @file translation_logic.test.ts
 * @description Vitest suite for English-First fallback strategy validation.
 */

import { describe, it, expect } from 'vitest';
import { getDisplayTask, parseTranslatedText } from '../logic';
import { Message } from '../types';

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

    it('should use the default language (en) when no language is specified', () => {
        const result = getDisplayTask(mockMessages[3] as Message);
        expect(result).toBe("Source Task");
    });
});

describe('parseTranslatedText (mirror of backend)', () => {
    it('returns plain text as the main task with no subtasks', () => {
        expect(parseTranslatedText('번역된 작업')).toEqual({ task: '번역된 작업', subtasks: [] });
    });

    it('parses a {t, s} JSON payload into main task and subtasks', () => {
        const raw = JSON.stringify({ t: '메인 작업', s: ['하위1', '하위2'] });
        expect(parseTranslatedText(raw)).toEqual({ task: '메인 작업', subtasks: ['하위1', '하위2'] });
    });

    it('parses a {t} payload without subtasks', () => {
        expect(parseTranslatedText(JSON.stringify({ t: '메인만' }))).toEqual({ task: '메인만', subtasks: [] });
    });

    it('falls back to raw string when JSON is malformed', () => {
        expect(parseTranslatedText('{not json')).toEqual({ task: '{not json', subtasks: [] });
    });

    it('handles empty input', () => {
        expect(parseTranslatedText('')).toEqual({ task: '', subtasks: [] });
    });
});
