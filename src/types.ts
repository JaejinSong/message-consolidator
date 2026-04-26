/**
 * @file types.ts
 * @description Centralized type definitions for Project GEM.
 */
 
export interface Subtask {
    task: string;
    assignee_id?: number;
    assignee?: string;
    done: boolean;
}

export interface Message {
    id: number;
    requester: string;
    task: string;
    task_en?: string;
    task_ko?: string;
    source: string;
    timestamp?: string;
    created_at?: string;
    done: boolean;
    completed_at?: string;
    assignee?: string;
    waiting_on?: string;
    category?: string;
    metadata?: string | Record<string, any> | null;
    is_translating?: boolean;
    translation_error?: string | null;
    has_original?: boolean;
    room?: string;
    user_email?: string;
    link?: string;
    source_ts?: string;
    is_deleted?: boolean | number;
    assigned_to?: string;
    source_channels?: string[];
    consolidated_context?: string[];
    subtasks?: Subtask[];
    deadline?: string;
}

export interface UserProfile {
    email: string;
    picture: string;
    name: string;
    archive_days?: number;
    aliases?: string[];
    points?: number;
    streak?: number;
    streak_freezes?: number;
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
    todayFiltered?: number;
    monthlyFiltered?: number;
}

export interface TimeSeriesPoint {
    date: string;
    counts: Record<string, number>;
}

export interface UserStats {
    pending_me: number;
    pending_others: number;
    total_completed: number;
    peak_time: string;
    abandoned_tasks: number;
    daily_completions: Record<string, number>;
    source_distribution: Record<string, number>;
    source_distribution_total: Record<string, number>;
    hourly_activity: Record<string, number>;
    completion_history: TimeSeriesPoint[];
}

export interface CategorizedMessages {
    inbox: Message[];
    delegated: Message[];
    reference: Message[];
}

export interface AppState {
    userProfile: UserProfile;
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
    archiveStatus: string;
    messages: CategorizedMessages;
    userStats: UserStats | null;
    selectedTaskIds: Set<number>;
    reports: Record<string, IReportData>;
    reportHistory: IReportData[];
    isFetchingMessages: boolean;
    isFetchingStatus: boolean;
    deadlineFilter: 'all' | 'today' | 'week' | 'has_deadline';
}



// Why: locale dictionaries grew organically with ~220 keys per language and a few array values
// (glance* templates, emptyStateMessages); a strict named interface would force every UI string
// to be enumerated. Looser per-entry typing keeps the gain at the dictionary level (typed access
// by lang code) without forcing exhaustive locale audits.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type I18nEntry = Record<string, any>;

export interface I18nDictionary {
    [lang: string]: I18nEntry;
}

export interface MessageHandlers {
    onToggleDone: (id: string, done: boolean) => Promise<void>;
    onToggleSubtask?: (taskId: string, subtaskIndex: number, done: boolean) => Promise<void>;
    onDeleteTask: (id: string) => Promise<void>;
    onShowOriginal: (id: string) => Promise<void>;
    onMapAlias?: (name: string, source: string) => void;
    onSelectTask?: (id: number, selected: boolean) => void;
}

export interface ServiceHandlers extends MessageHandlers {
    onWhatsAppLogout: () => Promise<void>;
    onWhatsAppRelink: () => Promise<void>;
    onGmailDisconnect: () => Promise<void>;
    onGmailConnect: () => void;

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

export interface ParsedVisualization {
    nodes: IReportNode[];
    links: IReportLink[];
}

export interface IReportData {
    id: number;
    title?: string;
    user_email: string;
    start_date: string;
    end_date: string;
    report_summary: string;
    translations?: Record<string, string>;
    visualization_data: string | ParsedVisualization;
    status?: 'processing' | 'completed' | 'failed';
    is_truncated?: boolean;
    created_at?: string;
}

export interface AccountItem {
    id: string | number;
    canonical_id: string;
    display_name?: string;
}

export interface ComboboxOptions {
    placeholder?: string;
    searchFn: (q: string) => Promise<AccountItem[]>;
    onSelect?: (item: AccountItem) => void;
    debounceMs?: number;
    renderItem?: (item: AccountItem) => string;
    id?: string;
}

export interface ComboboxInterface {
    getValue(): AccountItem | null;
    clear(): void;
    destroy(): void;
}
