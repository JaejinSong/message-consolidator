/**
 * @file state.ts
 * @description Centralized application state management with TypeScript.
 */

import { AppState, UserProfile, Message, CategorizedMessages, IReportData } from './types.ts';

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
    archiveStatus: 'all',
    messages: { inbox: [], pending: [], waiting: [] },
    userStats: null,
    selectedTaskIds: new Set<number>(),
    reports: {},
    reportHistory: []
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

/**
 * Explicitly sets or toggles a task selection.
 */
export const setTaskSelection = (id: number, isSelected: boolean): void => {
    if (isSelected) {
        state.selectedTaskIds.add(id);
        console.log(`[DEBUG] state.ts - Task ${id} ADDED. Total: ${state.selectedTaskIds.size}`);
    } else {
        state.selectedTaskIds.delete(id);
        console.log(`[DEBUG] state.ts - Task ${id} REMOVED. Total: ${state.selectedTaskIds.size}`);
    }
};

/**
 * Toggles a task ID in the selection set.
 */
export const toggleTaskSelection = (id: number): void => {
    const isSelected = state.selectedTaskIds.has(id);
    setTaskSelection(id, !isSelected);
};

/**
 * Clears all current task selections.
 */
export const clearTaskSelection = (): void => {
    state.selectedTaskIds.clear();
};

/**
 * Why: Upserts a report into the indexed state for O(1) retrieval by date range.
 */
export const upsertReport = (report: IReportData): void => {
    if (!report.start_date || !report.end_date) return;
    const key = `${report.start_date}_${report.end_date}`;
    state.reports[key] = { ...state.reports[key], ...report };
};

/**
 * Updates the report history metadata list.
 */
export const updateReportHistory = (history: IReportData[]): void => {
    state.reportHistory = history || [];
};
