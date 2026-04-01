/**
 * @file state.ts
 * @description Centralized application state management with TypeScript.
 */

import { AppState, UserProfile, Message } from './types.ts';

export const state: AppState = {
    userProfile: { email: "", picture: "", name: "", points: 0, streak: 0, streak_freezes: 0 },
    userAliases: [],
    currentLang: localStorage.getItem('mc_lang') || 'ko',
    currentTheme: localStorage.getItem('mc_theme') || 'dark',
    waConnected: false,
    gmailConnected: false,
    archivePage: 1,
    archiveLimit: 20,
    archiveSearch: "",
    archiveSort: '',
    archiveOrder: 'DESC',
    archiveTotalCount: 0,
    archiveThresholdDays: 7, 
    messages: []
};

/**
 * Updates application language and persists to localStorage.
 */
export const updateLang = (lang: string): void => {
    state.currentLang = lang;
    localStorage.setItem('mc_lang', lang);
};

/**
 * Updates application theme and persists to localStorage.
 */
export const updateTheme = (theme: string): void => {
    state.currentTheme = theme;
    localStorage.setItem('mc_theme', theme);
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
 */
export const updateMessages = (messages: any[]): void => {
    state.messages = messages || [];
};
