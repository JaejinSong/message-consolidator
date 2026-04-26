/**
 * @file storage.ts
 * @description Centralized localStorage wrapper. Single source of truth for
 * persistent client-side keys; protects against typos/key-drift and provides
 * a single seam for future migration logic (e.g. namespacing, encryption).
 *
 * NOTE: index.html runs an inline `localStorage.getItem('mc_theme')` bootstrap
 * before this module loads (FOUC prevention). That literal MUST stay in sync
 * with STORAGE_KEYS.THEME below.
 */

export const STORAGE_KEYS = {
    LANG: 'mc_lang',
    THEME: 'mc_theme',
} as const;

export type StorageKey = typeof STORAGE_KEYS[keyof typeof STORAGE_KEYS];

const isAvailable = (): boolean => typeof localStorage !== 'undefined';

export const storage = {
    get(key: StorageKey, fallback: string): string {
        if (!isAvailable()) return fallback;
        return localStorage.getItem(key) ?? fallback;
    },
    set(key: StorageKey, value: string): void {
        if (!isAvailable()) return;
        localStorage.setItem(key, value);
    },
    remove(key: StorageKey): void {
        if (!isAvailable()) return;
        localStorage.removeItem(key);
    },
};
