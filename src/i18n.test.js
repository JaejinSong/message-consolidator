import { describe, it, expect } from 'vitest';
import { t, getLocale } from './i18n';
import { I18N_DATA } from './locales';

describe('i18n.ts - t()', () => {
    it('returns the correct EN string for a known key', () => {
        expect(t('myTasks', 'en')).toBe(I18N_DATA['en'].myTasks);
    });

    it('returns the correct KO string for a known key', () => {
        expect(t('myTasks', 'ko')).toBe(I18N_DATA['ko'].myTasks);
    });

    it('falls back to EN when lang is empty string', () => {
        expect(t('myTasks', '')).toBe(I18N_DATA['en'].myTasks);
    });

    it('falls back to EN when lang is undefined/missing', () => {
        expect(t('myTasks')).toBe(I18N_DATA['en'].myTasks);
    });

    it('falls back to EN when lang is unknown locale', () => {
        expect(t('myTasks', 'zz')).toBe(I18N_DATA['en'].myTasks);
    });

    it('returns the key itself when key does not exist in any locale', () => {
        expect(t('nonExistentKey_xyz', 'en')).toBe('nonExistentKey_xyz');
    });
});

describe('i18n.ts - getLocale()', () => {
    it('returns the EN locale object for "en"', () => {
        expect(getLocale('en')).toEqual(I18N_DATA['en']);
    });

    it('returns the KO locale object for "ko"', () => {
        expect(getLocale('ko')).toEqual(I18N_DATA['ko']);
    });

    it('falls back to EN for unknown locale', () => {
        expect(getLocale('zz')).toEqual(I18N_DATA['en']);
    });

    it('falls back to EN when called with no argument', () => {
        expect(getLocale()).toEqual(I18N_DATA['en']);
    });
});
