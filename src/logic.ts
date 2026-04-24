import { I18N_DATA } from './locales';
import { TimeService } from './utils';
import { state } from './state';
import { marked } from 'marked';

/**
 * @file logic.ts
 * @description Pure functions for data processing, sorting, and classification.
 */

import { Message, I18nDictionary, IReportData, ParsedVisualization } from './types';

/** 완료된 업무가 대시보드에 노출되는 기준일 (보관함 이관 기준) */
export const getArchiveThresholdDays = (): number => state.archiveThresholdDays || 7;

/**
 * Sorts and filters messages based on the current search query.
 */
export function sortAndSearchMessages(messages: Message[], searchQuery: string): Message[] {
    if (!messages) return [];

    const now = new Date();
    const thresholdDays = getArchiveThresholdDays();
    const q = searchQuery ? searchQuery.toLowerCase() : '';

    return messages.filter(m => {
        // 1. Archive threshold check
        if (m.done) {
            const ts = m.completed_at || m.timestamp || m.created_at;
            if (ts && TimeService.getDiffInDays(new Date(ts), now) > thresholdDays) {
                return false;
            }
        }

        // 2. Filter by Search Query
        if (q) {
            const taskStr = (m.task || "").toLowerCase();
            const reqStr = (m.requester || "").toLowerCase();
            if (!taskStr.includes(q) && !reqStr.includes(q)) return false;
        }

        return true;
    }).sort((a, b) => {
        // 1순위 정렬: 완료된(done) 업무는 맨 아래로 강제 이동
        if (a.done !== b.done) {
            return a.done ? 1 : -1;
        }
        // 2순위 정렬: 생성일 기준 최신순
        const tsA = a.timestamp || a.created_at || "0";
        const tsB = b.timestamp || b.created_at || "0";
        return new Date(tsB).getTime() - new Date(tsA).getTime();
    });
}

/**
 * Returns the count of active messages (not done, not deleted).
 * Why: Derived state for UI counters to show only actionable tasks.
 */
export function getActiveCount(messages: Message[] | undefined): number {
    if (!messages) return 0;
    return messages.filter(m => !m.done && !m.is_deleted).length;
}



/**
 * Generates continuous heatmap data for the last X days.
 */
export function generateHeatmapData(history: any[], days: number = 30) {
    const historyMap: Record<string, { total: number; counts: any }> = {};
    if (history && Array.isArray(history)) {
        history.forEach(p => {
            if (!p.date || !p.counts) return;
            const sum = Object.values(p.counts as Record<string, number>).reduce((a, b) => a + (Number(b) || 0), 0);
            historyMap[p.date] = { total: sum, counts: p.counts };
        });
    }

    const today = new Date();
    today.setHours(0, 0, 0, 0);

    return Array.from({ length: days }, (_, i) => {
        const d = new Date(today);
        d.setDate(d.getDate() - (days - 1 - i));
        const ds = TimeService.getLocalDateString(d);
        const data = historyMap[ds] || { total: 0, counts: {} };
        return { date: ds, count: data.total, counts: data.counts, level: calculateHeatmapLevel(data.total) };
    });
}


/**
 * Calculates activity level for heatmap.
 */
export function calculateHeatmapLevel(count: number): number {
    if (count <= 0) return 0;
    if (count < 3) return 1;
    if (count < 5) return 2;
    if (count < 8) return 3;
    return 4;
}

/**
 * Processes raw completion history into a continuous timeline.
 */
export function processTimeSeriesData(history: { date: string; counts: Record<string, number> }[], days: number): any[] {
    const result: any[] = [];
    const today = new Date();
    today.setHours(0, 0, 0, 0);

    const historyMap: Record<string, Record<string, number>> = {};
    let cumulative = 0;

    if (history && Array.isArray(history)) {
        const cutoffDate = new Date(today);
        cutoffDate.setDate(cutoffDate.getDate() - days);
        const cutoffStr = TimeService.getLocalDateString(cutoffDate);

        history.forEach(item => {
            historyMap[item.date] = item.counts || {};
            if (item.date < cutoffStr) {
                const values = Object.values(item.counts || {}) as number[];
                cumulative += values.reduce((a: number, b: number) => a + b, 0);
            }
        });
    }

    for (let i = days - 1; i >= 0; i--) {
        const d = new Date(today);
        d.setDate(d.getDate() - i);
        const dateStr = TimeService.getLocalDateString(d);

        const counts = historyMap[dateStr] || {};
        const values = Object.values(counts) as number[];
        const total = values.reduce((a: number, b: number) => a + b, 0);

        cumulative += total;

        result.push({
            date: dateStr, counts, total, cumulative
        });
    }

    return result;
}

/**
 * Gets the deadline badge HTML based on a scheduled event date (YYYY-MM-DD).
 * Shows urgency relative to today: today / tomorrow / D-N (2-7 days) / past.
 */
export function getDeadlineBadge(deadline: string | undefined, isDone: boolean, lang: string = 'ko'): string {
    if (isDone || !deadline) return '';

    const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];
    const todayStr = new Date().toISOString().slice(0, 10);
    const today = new Date(todayStr);
    const target = new Date(deadline.slice(0, 10));
    if (isNaN(target.getTime())) return '';

    const diffDays = Math.round((target.getTime() - today.getTime()) / 86400000);

    if (diffDays === 0) {
        return `<span class="c-badge c-badge--deadline-today">${i18n.deadlineToday ?? '오늘'}</span>`;
    }
    if (diffDays === 1) {
        return `<span class="c-badge c-badge--deadline-tomorrow">${i18n.deadlineTomorrow ?? '내일'}</span>`;
    }
    if (diffDays >= 2 && diffDays <= 7) {
        return `<span class="c-badge c-badge--deadline-soon">${i18n.deadlineSoon ?? 'D-'}${diffDays}</span>`;
    }
    if (diffDays < 0 && diffDays >= -7) {
        return `<span class="c-badge c-badge--deadline-past">${i18n.deadlinePast ?? '지남'}</span>`;
    }
    return '';
}

/**
 * Custom markdown to HTML parser for release notes and descriptions.
 * Uses marked.js with line breaks enabled for better consistency with AI output.
 */
export function parseMarkdown(text: string): string {
    if (!text) return '';
    
    // Set options for consistent newline handling
    marked.use({
        breaks: true,
        gfm: true
    });

    return marked.parse(text) as string;
}


/**
 * Why: Returns the appropriate task description based on language availability.
 * Follows English-First with Korean fallback strategy.
 */
export function getDisplayTask(m: Message, lang?: string): string {
    const targetLang = lang || 'en';
    
    // 1. If translation exists (and not in EN tab), use it.
    // Why: m.task_ko is the JIT translation slot for current UI language.
    if (targetLang !== 'en' && m.task_ko) {
        return m.task_ko;
    }
    
    // 2. Default to Canonical English (task_en or task)
    return m.task_en || m.task || "";
}

/**
 * Why: Normalizes raw report data with safe JSON parsing for visualization.
 */
export function normalizeReportData(data: any): IReportData {
    if (!data) return {} as IReportData;

    let viz: ParsedVisualization = { nodes: [], links: [] };
    const rawViz = data.visualization || data.visualization_data;

    if (typeof rawViz === 'string' && rawViz.trim()) {
        try { viz = JSON.parse(rawViz); } catch (e) { /* Fallback */ }
    } else if (rawViz && typeof rawViz === 'object') {
        viz = rawViz as ParsedVisualization;
    }

    const reportSummary = data.report_summary || "";

    return {
        id: Number(data.id),
        user_email: data.user_email || "",
        start_date: data.start_date || "",
        end_date: data.end_date || "",
        report_summary: reportSummary,
        translations: data.translations || {},
        visualization_data: viz,
        is_truncated: Boolean(data.is_truncated),
        status: data.status || "completed",
        created_at: data.created_at || ""
    };
}

/**
 * Why: Language-aware summary extractor with fallback.
 */
export function getReportSummary(report: IReportData, lang: string): string {
    if (!report) return "";
    if (report.translations && report.translations[lang]) {
        return report.translations[lang];
    }
    return report.report_summary || "";
}
