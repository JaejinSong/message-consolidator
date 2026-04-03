/**
 * @file state.ts
 * @description Centralized application state management with TypeScript.
 */

import { AppState, UserProfile, Message, CategorizedMessages } from './types.ts';

export const state: AppState = {
    userProfile: { email: "", picture: "", name: "", points: 0, streak: 0, streak_freezes: 0 },
    userAliases: [],
    currentLang: (typeof localStorage !== 'undefined') ? (localStorage.getItem('mc_lang') || 'ko') : 'ko',
    currentTheme: (typeof localStorage !== 'undefined') ? (localStorage.getItem('mc_theme') || 'dark') : 'dark',
    waConnected: false,
    gmailConnected: false,
    archivePage: 1,
    archiveLimit: 20,
    archiveSearch: "",
    archiveSort: '',
    archiveOrder: 'DESC',
    archiveTotalCount: 0,
    archiveThresholdDays: 7, 
    messages: { inbox: [], pending: [], waiting: [] },
    userStats: null
};

/**
 * Updates application language and persists to localStorage.
 */
export const updateLang = (lang: string): void => {
    state.currentLang = lang;
    if (typeof localStorage !== 'undefined') {
        localStorage.setItem('mc_lang', lang);
    }
};

/**
 * Updates application theme and persists to localStorage.
 */
export const updateTheme = (theme: string): void => {
    state.currentTheme = theme;
    if (typeof localStorage !== 'undefined') {
        localStorage.setItem('mc_theme', theme);
    }
};

/**
 * Updates user statistics and profile information.
 */
export const updateStats = (user: Partial<UserProfile> | null): void => {
    if (!user) return;
    if (user.archive_days !== undefined) {
        state.archiveThresholdDays = user.archive_days;
    }
    state.userProfile = { ...state.userProfile, ...user } as UserProfile;
};

/**
 * Updates the global messages list.
 * Why: Ensuring idempotency by replacing the entire categorized object, 
 * as it's the single source of truth from the backend. 
 */
export const updateMessages = (messages: CategorizedMessages): void => {
    state.messages = messages || { inbox: [], pending: [], waiting: [] };
};

/**
 * Generic Upsert Utility for state arrays.
 * Why: Preventing duplicates by checking for existing IDs before insertion.
 */
export function upsertItem<T extends { id: string | number }>(collection: T[], item: T): T[] {
    const index = collection.findIndex(i => i.id === item.id);
    if (index === -1) {
        return [...collection, item];
    }
    const next = [...collection];
    next[index] = { ...next[index], ...item };
    return next;
}
