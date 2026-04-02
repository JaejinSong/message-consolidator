/**
 * @file types.ts
 * @description Centralized type definitions for Project GEM.
 */

export interface Message {
    id: number;
    requester: string;
    task: string;
    source: string;
    timestamp?: string;
    created_at?: string;
    done: boolean;
    completed_at?: string;
    assignee?: string;
    waiting_on?: string;
    category?: string;
    metadata?: string | Record<string, any> | null;
    translating?: boolean;
    translationError?: string | null;
    has_original?: boolean;
    room?: string;
    user_email?: string;
    link?: string;
    source_ts?: string;
    is_deleted?: boolean | number;
}

export interface UserProfile {
    email: string;
    picture: string;
    name: string;
    points: number;
    streak: number;
    streak_freezes: number;
    archive_days?: number;
    xp?: number;
    level?: number;
}

export interface TokenUsage {
    todayTotal: number;
    todayPrompt: number;
    todayCompletion: number;
    todayCost: number;
    monthlyTotal: number;
    monthlyPrompt: number;
    monthlyCompletion: number;
    monthlyCost: number;
    model: string;
}

export interface UserStats {
    total_completed: number;
    peak_time: string;
    abandoned_tasks: number;
    daily_completions: Record<string, number>;
    source_distribution: Record<string, number>;
    source_distribution_total: Record<string, number>;
    pending_me: number;
    hourly_activity: Record<string, number>;
    completion_history: any[];
    max_daily_completed?: number;
    early_bird_count?: number;
}

export interface AppState {
    userProfile: UserProfile;
    userAliases: string[];
    currentLang: string;
    currentTheme: string;
    waConnected: boolean;
    gmailConnected: boolean;
    archivePage: number;
    archiveLimit: number;
    archiveSearch: string;
    archiveSort: string;
    archiveOrder: 'ASC' | 'DESC';
    archiveTotalCount: number;
    archiveThresholdDays: number;
    messages: Message[];
}

export interface AchievementEntry {
    name: string;
    description?: string;
}

export interface I18nEntry {
    subTitle?: string;
    realTimeTasks?: string;
    scanNow?: string;
    scanning?: string;
    noTasks?: string;
    viewOriginal?: string;
    markDone?: string;
    delete?: string;
    assigneeMe?: string;
    originalNotAvailable?: string;
    logoutConfirm?: string;
    disconnectConfirm?: string;
    policyLabel?: string;
    queryLabel?: string;
    promise?: string;
    waiting?: string;
    emptyStateMessages?: string[];
    waConnected?: string;
    qrError?: string;
    error?: string;
    generating?: string;
    achievements?: Record<string, AchievementEntry>;
}

export interface I18nDictionary {
    [lang: string]: I18nEntry;
}

export interface MessageHandlers {
    onToggleDone: (id: string, done: boolean) => Promise<void>;
    onDeleteTask: (id: string) => Promise<void>;
    onShowOriginal: (id: string) => Promise<void>;
    onMapAlias?: (name: string, source: string) => void;
}

export interface ServiceHandlers extends MessageHandlers {
    onWhatsAppLogout: () => Promise<void>;
    onWhatsAppRelink: () => Promise<void>;
    onGmailDisconnect: () => Promise<void>;
    onGmailConnect: () => void;
    onBuyFreeze?: () => void;
}

/**
 * Insights Reporting Interfaces
 */
export interface IReportNode {
    id: string;
    name: string;
    value: number;
    category?: string;
    is_me?: boolean;
}

export interface IReportLink {
    source: string;
    target: string;
    value: number;
}

export interface IReportData {
    id: number;
    start_date: string;
    end_date: string;
    title?: string;
    report_summary?: string;
    summary?: string;
    translations?: Record<string, string>;
    visualization_data: string | { nodes: IReportNode[]; links: IReportLink[] };
    is_truncated?: boolean;
}
