/**
 * @file i18n.ts
 * @description Pure translation logic. No DOM manipulation.
 * DOM rendering is delegated to src/renderers/i18n-renderer.ts.
 */

import { I18N_DATA } from './locales';

const DEFAULT_LANG = 'en';

/**
 * Returns the localized string for the given key and language.
 * Pure function — no side effects.
 */
export function t(key: string, lang: string = DEFAULT_LANG): string {
    const locale = lang && I18N_DATA[lang] ? lang : DEFAULT_LANG;
    const data = I18N_DATA[locale] ?? I18N_DATA[DEFAULT_LANG];
    
    // Support nested path (e.g. "filterLabels.channel")
    const val = key.split('.').reduce<unknown>((obj, k) => (obj as Record<string, unknown> | undefined)?.[k], data);
    if (typeof val === 'string') return val;

    // Fallback if not found in target lang, try default lang
    const fallbackData = I18N_DATA[DEFAULT_LANG];
    const fallbackVal = key.split('.').reduce<unknown>((obj, k) => (obj as Record<string, unknown> | undefined)?.[k], fallbackData);
    return typeof fallbackVal === 'string' ? fallbackVal : key;
}

/**
 * Returns the entire locale data object for a given language.
 * Falls back to 'en' if the requested locale is unavailable.
 */
export function getLocale(lang: string = DEFAULT_LANG): Record<string, any> {
    return I18N_DATA[lang] ?? I18N_DATA[DEFAULT_LANG] ?? {};
}
