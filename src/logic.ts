import { I18N_DATA } from './locales.js';
import { ICONS } from './icons.ts';
import { TimeService } from './utils.ts';
import { state } from './state.ts';

/**
 * @file logic.ts
 * @description Pure functions for data processing, sorting, and classification.
 */

import { Message, I18nDictionary, IReportData, ParsedVisualization } from './types.ts';

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



export function calculateStats(messages: Message[]) {
    const total = messages.length;
    const completed = messages.filter(m => m.done).length;
    const categories: Record<string, number> = {};
    
    messages.forEach(m => {
        const cat = m.category || 'TASK';
        categories[cat] = (categories[cat] || 0) + 1;
    });

    const counts = messages.reduce((acc: Record<string, number>, m) => {
        const source = m.source || 'unknown';
        acc[source] = (acc[source] || 0) + 1;
        return acc;
    }, {});

    return { total, completed, categories, counts };
}

/**
 * Why: Logic to find recent trends for dashboard visualization.
 */
export function getRecentTrends(messages: Message[]): Record<string, number> {
    const trends: Record<string, number> = {};
    
    messages.forEach(m => {
        const d = new Date(m.timestamp || m.created_at || "");
        const dayStr = d.toISOString().split('T')[0];
        trends[dayStr] = (trends[dayStr] || 0) + 1;
    });

    return trends;
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
 * Calculates distribution percentages for different sources.
 */
export function calculateSourceDistribution(distributionMap: Record<string, number> = {}): Record<string, number> {
    const values = Object.values(distributionMap) as number[];
    const total = values.reduce((a: number, b: number) => a + b, 0);
    if (total === 0) return {};

    const result: Record<string, number> = {};
    let currentSum = 0;
    const entries = Object.entries(distributionMap).sort((a, b) => b[1] - a[1]);

    entries.forEach(([key, val], index) => {
        if (index === entries.length - 1) {
            result[key] = 100 - currentSum;
        } else {
            const p = Math.round((val / total) * 100);
            result[key] = p;
            currentSum += p;
        }
    });
    return result;
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
 * Gets the deadline badge HTML.
 */
export function getDeadlineBadge(timestamp: string | undefined, isDone: boolean, lang: string = 'ko'): string {
    if (isDone || !timestamp) return '';

    const start = new Date(timestamp);
    const now = new Date();
    if (start >= now) return '';

    let diffMs = now.getTime() - start.getTime();
    let current = new Date(start);
    let weekendDays = 0;

    current.setHours(0, 0, 0, 0);
    let endObj = new Date(now);
    endObj.setHours(0, 0, 0, 0);

    while (current < endObj) {
        current.setDate(current.getDate() + 1);
        if (current.getDay() === 0 || current.getDay() === 6) weekendDays++;
    }

    const diffHours = (diffMs - (weekendDays * 24 * 60 * 60 * 1000)) / (1000 * 60 * 60);
    const i18n = (I18N_DATA as I18nDictionary)[lang] || (I18N_DATA as I18nDictionary)['ko'];

    if (diffHours >= 72) {
        return `<span class="c-badge c-badge--priority-high">${ICONS.abandoned}${i18n.abandoned}</span>`;
    }
    if (diffHours >= 24) {
        return `<span class="c-badge c-badge--priority-medium">${ICONS.stale}${i18n.stale}</span>`;
    }
    return '';
}

/**
 * Custom markdown to HTML parser for release notes and descriptions.
 */
export function parseMarkdown(text: string): string {
    if (!text) return '';
    return text
        .replace(/^### (.*$)/gim, `<h3 style="margin-top: var(--spacing-2xl); margin-bottom: var(--spacing-sm); color: var(--text-main);">$1</h3>`)
        .replace(/^## (.*$)/gim, `<h2 style="margin-top: 1.8rem; margin-bottom: var(--spacing-md); color: var(--text-main); border-bottom: var(--border-thin) solid var(--glass-border); padding-bottom: var(--spacing-xs);">$1</h2>`)
        .replace(/^# (.*$)/gim, `<h1 style="margin-top: var(--spacing-3xl); margin-bottom: var(--spacing-lg); color: var(--accent-color); font-size: 1.4rem;">$1</h1>`)
        .replace(/^\-\-\-/gim, '<hr class="settings-divider" style="margin: var(--spacing-3xl) 0;">')
        .replace(/\*\*(.*?)\*\*/gim, '<strong style="color: var(--text-main); font-weight: 800;">$1</strong>')
        .replace(/`(.*?)`/gim, '<code style="background: var(--glass-border); padding: var(--spacing-xxs) var(--spacing-xs); border-radius: var(--radius-sm); font-size: 0.9em; font-family: monospace;">$1</code>')
        .replace(/^\- (.*$)/gim, `<div style="padding-left: var(--spacing-lg); text-indent: -0.8rem; margin-bottom: var(--spacing-sm); color: var(--text-dim); line-height: 1.6;"><span style="color: var(--accent-color);">•</span> $1</div>`)
        .replace(/\n/gim, '<br>')
        .replace(/(<\/h[1-3]>|<hr.*?>|<\/div>)<br>/gim, '$1'); 
}


/**
 * Why: Returns the appropriate task description based on language availability.
 * Follows English-First with Korean fallback strategy.
 */
export function getDisplayTask(m: Message, lang?: string): string {
    const targetLang = lang || 'ko';
    
    // 1. If Korean requested and exists, use it
    if (targetLang === 'ko' && m.task_ko) {
        return m.task_ko;
    }
    
    // 2. Fallback to English 원문 (task_en or task)
    return m.task_en || m.task || "";
}

/**
 * Why: Normalizes raw report data with safe JSON parsing for visualization.
 */
export function normalizeReportData(data: any): IReportData {
    if (!data) return {} as IReportData;

    let viz: ParsedVisualization = { nodes: [], links: [] };
    const rawViz = data.visualization_data;

    if (typeof rawViz === 'string' && rawViz.trim()) {
        try { viz = JSON.parse(rawViz); } catch (e) { /* Fallback used */ }
    } else if (rawViz && typeof rawViz === 'object') {
        viz = rawViz;
    }

    return {
        id: Number(data.id),
        user_email: data.user_email || "",
        start_date: data.start_date || "",
        end_date: data.end_date || "",
        report_summary: data.report_summary || "",
        translations: data.translations || {},
        visualization_data: viz,
        is_truncated: Boolean(data.is_truncated),
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
