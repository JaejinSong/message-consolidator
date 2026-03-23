import { I18N_DATA } from './locales.js';
import { ICONS } from './icons.js';

/**
 * @file logic.js
 * @description Pure functions for data processing, sorting, and classification.
 * This module is decoupled from the DOM and can be tested in a Node.js environment.
 */

/**
 * @typedef {Object} Message
 * @property {number} id
 * @property {string} requester
 * @property {string} task
 * @property {string} source
 * @property {string} timestamp
 * @property {boolean} done
 * @property {string} [completed_at]
 * @property {string} [assignee]
 * @property {string} [waiting_on]
 */

/** 완료된 업무가 대시보드에 노출되는 기준일 (보관함 이관 기준) */
export const ARCHIVE_THRESHOLD_DAYS = 1;

/**
 * Sorts and filters messages based on the current view and search query.
 * @param {Message[]} messages - Array of message objects.
 * @param {string} currentTab - Current active tab ID.
 * @param {string} searchQuery - Search query string.
 * @returns {Message[]} Filtered and sorted messages.
 */
export function sortAndFilterMessages(messages, currentTab, searchQuery) {
    if (!messages) return [];

    // 1일 이내 완료된 업무인지 확인 (완료일이 없으면 생성일 기준)
    const isVisible = (m) => {
        if (!m.done) return true;
        const ts = m.completed_at || m.timestamp || m.created_at;
        if (!ts) return true; // 예외 처리: 날짜 정보가 전혀 없는 경우
        const refDate = new Date(ts);
        const diffDays = (new Date() - refDate) / (1000 * 60 * 60 * 24);
        return diffDays <= ARCHIVE_THRESHOLD_DAYS;
    };

    let filtered = messages.filter(isVisible);

    // Filter by Tab
    if (currentTab === 'myTasksTab') {
        filtered = filtered.filter(m => !m.waiting_on && m.assignee === 'me');
    } else if (currentTab === 'otherTasksTab') {
        filtered = filtered.filter(m => !m.waiting_on && m.assignee !== 'me');
    } else if (currentTab === 'waitingTasksTab') {
        filtered = filtered.filter(m => m.waiting_on);
    }

    // Filter by Search Query
    if (searchQuery) {
        const q = searchQuery.toLowerCase();
        filtered = filtered.filter(m =>
            m.task.toLowerCase().includes(q) ||
            m.requester.toLowerCase().includes(q)
        );
    }

    // Sort by Timestamp/Created (Newest first)
    return filtered.sort((a, b) => {
        // 1순위 정렬: 완료된(done) 업무는 맨 아래로 강제 이동
        if (a.done !== b.done) {
            return a.done ? 1 : -1;
        }
        // 2순위 정렬: 생성일 기준 최신순
        const tsA = a.timestamp || a.created_at || 0;
        const tsB = b.timestamp || b.created_at || 0;
        return new Date(tsB) - new Date(tsA);
    });
}

/**
 * Classifies messages into categories for dashboard summary.
 * @param {Message[]} messages - Array of message objects.
 * @returns {Object} Count per category.
 */
export function classifyMessages(messages) {
    const counts = {
        my: 0,
        others: 0,
        waiting: 0,
        all: 0
    };

    if (!messages) return counts;

    messages.forEach(m => {
        if (m.done) return;
        counts.all++;
        if (m.waiting_on) {
            counts.waiting++;
        } else if (m.assignee === 'me') {
            counts.my++;
        } else {
            counts.others++;
        }
    });

    return counts;
}

/**
 * Calculates activity level for heatmap based on task count.
 * @param {number} count - Number of completed tasks for a day.
 * @returns {number} Level from 0 to 4.
 */
export function calculateHeatmapLevel(count) {
    if (count <= 0) return 0;
    if (count < 3) return 1;
    if (count < 5) return 2;
    if (count < 8) return 3;
    return 4;
}

/**
 * Calculates distribution percentages for different sources.
 * @param {Object} distributionMap - Object with source keys and activity counts.
 * @returns {Object} Standardized percentages (total 100).
 */
export function calculateSourceDistribution(distributionMap = {}) {
    const total = Object.values(distributionMap).reduce((a, b) => a + b, 0);
    if (total === 0) return {};

    const result = {};
    let currentSum = 0;
    const entries = Object.entries(distributionMap).sort((a, b) => b[1] - a[1]); // 오차 보정을 위해 큰 값부터 정렬

    entries.forEach(([key, val], index) => {
        if (index === entries.length - 1) {
            result[key] = 100 - currentSum; // 마지막 채널이 남은 %를 모두 가져가 정확히 100% 보장
        } else {
            const p = Math.round((val / total) * 100);
            result[key] = p;
            currentSum += p;
        }
    });
    return result;
}


/**
 * Processes raw completion history into a continuous timeline for charts.
 * @param {Array} history - Array of { date: 'YYYY-MM-DD', counts: { slack: n, ... } }
 * @param {number} days - Number of past days to generate
 * @returns {Array} Continuous array with zero-filled gaps and cumulative totals
 */
export function processTimeSeriesData(history, days) {
    const result = [];
    const today = new Date();
    today.setHours(0, 0, 0, 0);

    const historyMap = {};
    let cumulative = 0;

    if (history && Array.isArray(history)) {
        const cutoffDate = new Date(today);
        cutoffDate.setDate(cutoffDate.getDate() - days);
        const cutoffStr = cutoffDate.toISOString().split('T')[0];

        history.forEach(item => {
            historyMap[item.date] = item.counts || {};
            // 이전 데이터들의 누적치를 초기 cumulative에 더함
            if (item.date < cutoffStr) {
                cumulative += Object.values(item.counts || {}).reduce((a, b) => a + b, 0);
            }
        });
    }

    for (let i = days - 1; i >= 0; i--) {
        const d = new Date(today);
        d.setDate(d.getDate() - i);
        const dateStr = d.toISOString().split('T')[0];

        const counts = historyMap[dateStr] || {};
        const total = Object.values(counts).reduce((a, b) => a + b, 0);

        cumulative += total;

        result.push({
            date: dateStr, counts, total, cumulative
        });
    }

    return result;
}

/**
 * Gets the deadline badge HTML based on the task timestamp.
 * @param {string} timestamp - ISO timestamp string.
 * @param {boolean} isDone - Whether the task is completed.
 * @param {string} lang - Current language ('ko' or 'en').
 * @returns {string} HTML string for the badge.
 */
export function getDeadlineBadge(timestamp, isDone, lang = 'ko') {
    if (isDone) return '';

    const start = new Date(timestamp);
    const now = new Date();
    if (start >= now) return '';

    let diffMs = now - start;
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

    if (diffHours >= 72) {
        return `<span class="badge badge-abandoned">${ICONS.abandoned}${I18N_DATA[lang].abandoned}</span>`;
    }
    if (diffHours >= 24) {
        return `<span class="badge badge-stale">${ICONS.stale}${I18N_DATA[lang].stale}</span>`;
    }
    return '';
}

/**
 * Custom markdown to HTML parser for release notes and descriptions.
 * @param {string} text - Raw markdown text.
 * @returns {string} Sanitized HTML.
 */
export function parseMarkdown(text) {
    if (!text) return '';
    return text
        .replace(/^### (.*$)/gim, '<h3 style="margin-top: 1.5rem; margin-bottom: 0.5rem; color: var(--text-main);">$1</h3>')
        .replace(/^## (.*$)/gim, '<h2 style="margin-top: 1.8rem; margin-bottom: 0.8rem; color: var(--text-main); border-bottom: 1px solid rgba(255,255,255,0.1); padding-bottom: 0.3rem;">$1</h2>')
        .replace(/^# (.*$)/gim, '<h1 style="margin-top: 2rem; margin-bottom: 1rem; color: var(--accent-color); font-size: 1.4rem;">$1</h1>')
        .replace(/^\-\-\-/gim, '<hr class="settings-divider" style="margin: 2rem 0;">')
        .replace(/\*\*(.*?)\*\*/gim, '<strong style="color: var(--text-main); font-weight: 800;">$1</strong>')
        .replace(/`(.*?)`/gim, '<code style="background: rgba(255,255,255,0.1); padding: 0.2rem 0.4rem; border-radius: 4px; font-size: 0.9em; font-family: monospace;">$1</code>')
        .replace(/^\- (.*$)/gim, '<div style="padding-left: 1rem; text-indent: -0.8rem; margin-bottom: 0.5rem; color: var(--text-dim); line-height: 1.6;"><span style="color: var(--accent-color);">•</span> $1</div>')
        .replace(/\n/gim, '<br>')
        .replace(/(<\/h[1-3]>|<hr.*?>|<\/div>)<br>/gim, '$1'); // 블록 요소 뒤 불필요한 줄바꿈 정리
}
