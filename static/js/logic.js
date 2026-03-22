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

/**
 * Sorts and filters messages based on the current view and search query.
 * @param {Message[]} messages - Array of message objects.
 * @param {string} currentTab - Current active tab ID.
 * @param {string} searchQuery - Search query string.
 * @returns {Message[]} Filtered and sorted messages.
 */
export function sortAndFilterMessages(messages, currentTab, searchQuery) {
    if (!messages) return [];

    // 6일 이내 완료된 업무인지 확인 (완료일이 없으면 생성일 기준)
    const isVisible = (m) => {
        if (!m.done) return true;
        const ts = m.completed_at || m.timestamp || m.created_at;
        if (!ts) return true; // 예외 처리: 날짜 정보가 전혀 없는 경우
        const refDate = new Date(ts);
        const diffDays = (new Date() - refDate) / (1000 * 60 * 60 * 24);
        return diffDays <= 6;
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
    const slack = distributionMap.slack || 0;
    const whatsapp = distributionMap.whatsapp || 0;
    const gmail = distributionMap.gmail || 0;

    const total = slack + whatsapp + gmail;
    if (total === 0) return { slack: 0, whatsapp: 0, gmail: 0 };

    const pSlack = Math.round((slack / total) * 100);
    const pWa = Math.round((whatsapp / total) * 100);
    const pGmail = 100 - pSlack - pWa; // Ensure sum is exactly 100

    return { slack: pSlack, whatsapp: pWa, gmail: pGmail };
}
