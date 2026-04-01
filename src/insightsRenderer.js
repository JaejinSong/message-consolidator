import { state } from './state.ts';
import { escapeHTML, TimeService } from './utils.ts';
import { I18N_DATA } from './locales.js';
import { calculateHeatmapLevel, calculateSourceDistribution, processTimeSeriesData } from './logic.ts';
import * as echarts from 'echarts';
import { marked } from 'marked';

/**
 * @file insightsRenderer.js
 * @description Handles all DOM rendering and visualizations for the Insights module.
 */

// Standalone utility to extract CSS variable values
export const getCssVariableValue = (name) => {
    const style = getComputedStyle(document.documentElement);
    return style.getPropertyValue(name).trim();
};

// Why: Centralized source color mapping using CSS theme variables. 
// Uses getters to ensure colors update dynamically when the theme changes.
export const SOURCE_COLORS = {
    get slack() { return getCssVariableValue('--color-slack') || 'rgb(54, 197, 240)'; },
    get whatsapp() { return getCssVariableValue('--color-whatsapp') || 'rgb(37, 211, 102)'; },
    get gmail() { return getCssVariableValue('--color-gmail') || 'rgb(234, 67, 53)'; },
    get default() { return getCssVariableValue('--color-source-default') || 'rgb(139, 92, 246)'; }
};

export const validateEdges = (nodes, links) => {
    const nodeIds = new Set((nodes || []).map(n => n.id));
    return (links || []).filter(l => {
        const src = l.source || l.from;
        const tgt = l.target || l.to;
        const isValid = nodeIds.has(src) && nodeIds.has(tgt);
        if (!isValid) {
            console.error(`[INSIGHTS] Edge Validation Failed: ${src} -> ${tgt}. One or both nodes missing from node set.`);
        }
        return isValid;
    });
};

// 동적 채널별 고유 색상 매핑 (CSS 변수 기반으로 초기화)
const getChannelColor = (channel) => {
    const varMap = {
        slack: '--color-slack',
        whatsapp: '--color-whatsapp',
        gmail: '--color-gmail'
    };
    return getCssVariableValue(varMap[channel.toLowerCase()] || '--color-source-default') || 'rgba(139, 92, 246, 1)';
};

export const insightsRenderer = {
    // Why: Utility to extract CSS variable values for ECharts components that do not support CSS variables directly.
    getCssVariableValue: (varName) => getCssVariableValue(varName),

    renderDailyGlance(stats) {
        const container = document.getElementById('dailyGlance');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        const getRandomMsg = (msg) => Array.isArray(msg) ? msg[Math.floor(Math.random() * msg.length)] : msg;
        let html = '<p>';
        const totalHtml = `<span class="c-insights-summary__accent">${stats.total_completed}</span>`;
        html += getRandomMsg(i18n.glanceTotalCompleted || "Total completed: {count}. ").replace('{count}', totalHtml);

        if (stats.peak_time && stats.peak_time !== "-") {
            const peakHtml = `<span class="c-insights-summary__accent">${stats.peak_time}</span>`;
            html += getRandomMsg(i18n.glancePeakTime || "Peak focus time: {time}. ").replace('{time}', peakHtml);
        }

        if (stats.abandoned_tasks > 0) {
            const abandonedText = getRandomMsg(i18n.glanceAbandoned || '⚠️ {count} items have been pending...').replace('{count}', `<span class="c-insights-summary__warning">${stats.abandoned_tasks}</span>`);
            html += `<br>${abandonedText}</p>`;
        } else {
            const clearText = getRandomMsg(i18n.glanceAllClear || '✨ All caught up! No stale tasks found.');
            html += `<br><span style="font-weight:600;">${clearText}</span></p>`;
        }
        container.className = 'c-insights-summary c-insights-card';
        container.innerHTML = html;
    },

    renderActivityHeatmap(stats) {
        const container = document.getElementById('activityHeatmap');
        if (!container) return;
        container.style.position = 'relative';

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        const today = new Date();
        const startDate = new Date(today);
        startDate.setDate(today.getDate() - 29);

        let html = '<div class="c-heatmap">';

        // 상단 요일 라벨 표시
        const daysOfWeek = lang === 'ko'
            ? ['일', '월', '화', '수', '목', '금', '토']
            : ['S', 'M', 'T', 'W', 'T', 'F', 'S'];

        let xLabels = '<div class="c-heatmap__grid" style="grid-template-columns: repeat(7, 1fr); font-size: 0.75rem; color: var(--text-dim); text-align: center; font-weight: 700; margin-bottom: 0.5rem;">';
        daysOfWeek.forEach(d => { xLabels += `<div>${d}</div>`; });
        xLabels += '</div>';

        html += xLabels;

        let gridHtml = '<div class="c-heatmap__grid" style="grid-template-columns: repeat(7, 1fr);">';

        // 시작 요일에 맞추어 앞부분 빈칸 채우기
        const startDay = startDate.getDay();
        for (let i = 0; i < startDay; i++) {
            gridHtml += `<div class="heatmap-day" style="background: transparent; box-shadow: none; cursor: default;"></div>`;
        }

        for (let i = 29; i >= 0; i--) {
            const d = new Date(today);
            d.setDate(d.getDate() - i);
            const dateStr = TimeService.getLocalDateString(d);

            const taskCount = stats.daily_completions[dateStr] || 0;
            const level = calculateHeatmapLevel(taskCount);
            const tooltipText = (i18n.heatmapTaskTooltip || "{count} tasks completed ({date})")
                .replace('{count}', taskCount).replace('{date}', dateStr);

            // 오늘 날짜 강조 클래스 추가
            const isToday = i === 0;
            const extraClass = isToday ? ' today-highlight' : '';

            gridHtml += `<div class="c-heatmap__day${extraClass}" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
        }

        gridHtml += '</div>';
        html += gridHtml + '</div><div class="chart-tooltip hidden" id="dailyHeatmapTooltip"></div>';

        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'dailyHeatmapTooltip');
    },

    renderSourceDistribution(stats) {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        const distActive = calculateSourceDistribution(stats.source_distribution);
        const distTotal = calculateSourceDistribution(stats.source_distribution_total);

        const renderBar = (dist, label) => {
            if (Object.keys(dist).length === 0) {
                return `<div class="distribution-row"><span class="dist-row-label">${label}</span><p class="empty-msg" style="margin:0.5rem 0;">No data</p></div>`;
            }

            let barsHtml = '', legendHtml = '';
            for (const [source, percentage] of Object.entries(dist)) {
                if (percentage > 0) {
                    const normalizedSource = source.toLowerCase();
                    const color = getChannelColor(normalizedSource);
                    // Why: Dynamic class naming with separator to avoid BEM linter prefix detection issues.
                    const segmentClass = 'c-stacked-bar__segment' + '--' + normalizedSource;

                    barsHtml += `<div class="c-stacked-bar__segment ${segmentClass}" style="width: ${percentage}%; background-color: ${color};"></div>`;
                    legendHtml += `<span style="color: ${color}; font-weight: 600; text-transform: capitalize;">${source} (${percentage}%)</span>`;
                }
            }

            return `
                <div class="distribution-row" style="margin-bottom: 1.5rem;">
                    <div class="dist-row-header" style="display:flex; justify-content:space-between; align-items:center; margin-bottom:0.5rem;">
                        <span class="dist-row-label" style="font-size:0.9rem; font-weight:700; color:var(--text-main);">${label}</span>
                    </div>
                    <div class="c-stacked-bar">${barsHtml}</div>
                    <div class="distribution-legend" style="display:flex; gap:1rem; flex-wrap:wrap; margin-top:0.5rem; font-size:0.75rem;">${legendHtml}</div>
                </div>
            `;
        };

        container.innerHTML = `
            ${renderBar(distTotal, i18n.sourceDistTotal || 'Total (incl. Archive)')}
            ${renderBar(distActive, i18n.sourceDistCurrent || 'Current Dashboard')}
        `;
        container.className = 'c-insights-summary c-insights-card';
    },

    renderWaitingMetrics(stats) {
        const meContainer = document.getElementById('waitingMetricsMe');
        const attentionContainer = document.getElementById('waitingMetricsAttention');
        if (meContainer) meContainer.textContent = stats.pending_me || 0;
        if (attentionContainer) attentionContainer.textContent = stats.abandoned_tasks || 0;
    },

    renderHourlyActivity(stats) {
        const container = document.getElementById('hourlyActivity');
        if (!container) return;
        if (!stats.hourly_activity || Object.keys(stats.hourly_activity).length === 0) {
            container.innerHTML = '<p class="empty-msg">Waiting for more completion data...</p>';
            return;
        }

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        let max = 0;
        for (let h = 0; h < 24; h++) {
            const paddedH = String(h).padStart(2, '0');
            const val = stats.hourly_activity[h] || stats.hourly_activity[paddedH] || stats.hourly_activity[String(h)] || 0;
            if (val > max) max = val;
        }

        const renderRow = (startHour, endHour, label) => {
            let cellsHtml = '';
            for (let h = startHour; h < endHour; h++) {
                const paddedH = String(h).padStart(2, '0');
                const count = stats.hourly_activity[h] || stats.hourly_activity[paddedH] || stats.hourly_activity[String(h)] || 0;
                const level = max > 0 ? Math.ceil((count / max) * 4) : 0;
                const tooltipText = (i18n.hourlyTaskTooltip || "{count} tasks completed ({time})").replace('{count}', count).replace('{time}', `${paddedH}:00`);
                cellsHtml += `<div class="c-heatmap__day" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
            }
            return `
                <div class="c-hourly-heatmap__row">
                    <span class="c-hourly-heatmap__label">${label}</span>
                    <div class="c-hourly-heatmap__grid">${cellsHtml}</div>
                </div>
            `;
        };

        let axisLabelsHtml = '';
        for (let i = 0; i < 12; i++) {
            axisLabelsHtml += `<div>${i === 0 ? '12' : i % 3 === 0 ? i : ''}</div>`;
        }

        const html = `
            <div class="c-hourly-heatmap">
                ${renderRow(0, 12, 'AM')}
                ${renderRow(12, 24, 'PM')}
                <div class="c-hourly-heatmap__row" style="margin-top: 0.125rem;"><span class="c-hourly-heatmap__label"></span><div class="c-hourly-heatmap__axis-labels">${axisLabelsHtml}</div></div>
            </div>
            <div class="chart-tooltip hidden" id="hourlyHeatmapTooltip"></div>`;

        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'hourlyHeatmapTooltip');
    },

    bindHeatmapTooltip(container, tooltipId) {
        const tooltip = container.querySelector('#' + tooltipId);
        if (!tooltip) return;

        // 툴팁 찌그러짐 방지 및 마우스 간섭(깜빡임) 제거
        tooltip.style.pointerEvents = 'none';
        tooltip.style.whiteSpace = 'nowrap';
        tooltip.style.width = 'max-content';
        tooltip.style.position = 'absolute';
        tooltip.style.zIndex = '1000';

        container.querySelectorAll('.c-heatmap__day').forEach(day => {
            day.addEventListener('mouseenter', (e) => {
                tooltip.innerHTML = `<div style="font-weight:600;">${e.currentTarget.dataset.tooltip}</div>`;
                tooltip.classList.remove('hidden');
            });
            day.addEventListener('mousemove', (e) => {
                const rect = container.getBoundingClientRect();
                const tooltipWidth = tooltip.offsetWidth || 100;
                const tooltipHeight = tooltip.offsetHeight || 40;
                const margin = 15;

                let left = e.clientX - rect.left;
                let top = e.clientY - rect.top;

                let finalLeft = (left + margin + tooltipWidth > rect.width) ? (left - tooltipWidth - margin) : (left + margin);
                let finalTop = (top + margin + tooltipHeight > rect.height) ? (top - tooltipHeight - margin) : (top + margin);

                finalLeft = Math.max(5, Math.min(finalLeft, rect.width - tooltipWidth - 5));
                finalTop = Math.max(5, Math.min(finalTop, rect.height - tooltipHeight - 5));

                tooltip.style.left = `${finalLeft}px`;
                tooltip.style.top = `${finalTop}px`;
            });
            day.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
        });
    },

    renderAchievements(allAch, userAch, stats) {
        const container = document.getElementById('achievementsList');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        const allList = Array.isArray(allAch) ? allAch : (allAch?.achievements || allAch?.data || []);
        const userList = Array.isArray(userAch) ? userAch : (userAch?.achievements || userAch?.data || []);
        if (!allList || allList.length === 0) {
            container.innerHTML = '<p class="empty-msg">No milestones found.</p>';
            return;
        }

        const userAchIds = new Set(userList.map(ua => ua.achievement_id));

        // Series configuration
        const SERIES_GROUPS = {
            'task-master': ['첫 걸음', '태스크 마스터 I', '태스크 마스터 II', '태스크 마스터 III'],
            'streak': ['스트릭 스타터', '끈기 끝판왕']
        };

        const belongsToSeries = (name) => {
            for (const [key, names] of Object.entries(SERIES_GROUPS)) {
                if (names.includes(name)) return key;
            }
            return null;
        };

        const grouped = new Map();
        const consolidated = [];

        allList.forEach(ach => {
            const seriesKey = belongsToSeries(ach.name);
            if (seriesKey) {
                if (!grouped.has(seriesKey)) grouped.set(seriesKey, []);
                grouped.get(seriesKey).push(ach);
            } else {
                consolidated.push(ach);
            }
        });

        // Consolidate each series
        grouped.forEach((achievements, key) => {
            // Sort by target_value to ensure tier order
            achievements.sort((a, b) => a.target_value - b.target_value);

            // Find the representative: first locked one, or the last unlocked one
            let representative = achievements[0];
            for (const ach of achievements) {
                representative = ach;
                if (!userAchIds.has(ach.id)) {
                    break;
                }
            }
            consolidated.push(representative);
        });

        // Sort: Unlocked first, then by target value or ID
        consolidated.sort((a, b) => {
            const aUnlocked = userAchIds.has(a.id);
            const bUnlocked = userAchIds.has(b.id);
            if (aUnlocked !== bUnlocked) return bUnlocked ? 1 : -1;
            return a.target_value - b.target_value;
        });

        const INITIAL_VISIBLE = 3;
        const totalCount = consolidated.length;

        let html = consolidated.map((ach, index) => {
            const isUnlocked = userAchIds.has(ach.id);
            const isHidden = index >= INITIAL_VISIBLE;

            let progress = 0;
            if (isUnlocked) {
                progress = ach.target_value;
            } else {
                switch (ach.criteria_type) {
                    case 'total_tasks': progress = stats?.total_completed || 0; break;
                    case 'level': progress = state.userProfile?.level || 1; break;
                    case 'early_bird': progress = stats?.early_bird_count || 0; break;
                    case 'daily_total': progress = stats?.max_daily_completed || 0; break;
                    case 'streak': progress = state.userProfile?.streak || 0; break;
                    default: progress = 0;
                }
            }
            const percent = Math.min(100, Math.round((progress / ach.target_value) * 100));

            const localizedName = i18n.achievements?.[ach.name]?.name || ach.name;
            const localizedDesc = i18n.achievements?.[ach.name]?.desc || ach.description;

            return `
                <div class="c-achievement ${isUnlocked ? 'c-achievement--unlocked' : 'c-achievement--locked'} ${isHidden ? 'hidden-ach' : ''}" 
                     style="${isHidden ? 'display: none;' : ''}">
                    <div class="c-achievement__icon">${ach.icon}</div>
                    <div class="achievement-info">
                        <div class="achievement-header">
                            <span class="c-achievement__title">${escapeHTML(localizedName)}</span>
                            ${isUnlocked ? `<span class="status-badge">${i18n.unlocked || 'Unlocked'}</span>` : ''}
                        </div>
                        <p class="achievement-desc">${escapeHTML(localizedDesc)}</p>
                        <div class="c-achievement__progress-base"><div class="c-achievement__progress-fill" style="width: ${percent}%"></div></div>
                        <div class="achievement-footer">
                            <span class="xp-reward">+${ach.xp_reward || 0} XP</span>
                            <span class="progress-text">${Math.min(progress, ach.target_value)}/${ach.target_value}</span>
                        </div>
                    </div>
                </div>`;
        }).join('');

        if (totalCount > INITIAL_VISIBLE) {
            html += `
                <div class="show-more-achievements">
                    <button id="btnShowMoreAch" class="glass-btn">
                        ${i18n.showMore || 'Show More'} <span class="count-pill">${totalCount - INITIAL_VISIBLE}</span>
                    </button>
                </div>
            `;
        }

        container.innerHTML = html;

        // Bind toggle event
        const btn = document.getElementById('btnShowMoreAch');
        if (btn) {
            btn.addEventListener('click', () => {
                const hiddenItems = container.querySelectorAll('.hidden-ach');
                const isExpanding = btn.dataset.expanded !== 'true';

                hiddenItems.forEach(item => {
                    item.style.display = isExpanding ? 'flex' : 'none';
                });

                btn.dataset.expanded = isExpanding ? 'true' : 'false';
                btn.innerHTML = isExpanding
                    ? `${i18n.showLess || 'Show Less'}`
                    : `${i18n.showMore || 'Show More'} <span class="count-pill">${totalCount - INITIAL_VISIBLE}</span>`;
            });
        }
    },

    renderAnkiChart(stats, currentChartDays) {
        if (!stats) return;
        const container = document.getElementById('ankiChartContainer');
        if (!container) return;
        container.style.position = 'relative'; // Set positioning context for the tooltip

        const history = stats.completion_history || [];
        const data = processTimeSeriesData(history, currentChartDays);

        const width = 800, height = 240;
        const pad = { t: 20, r: 55, b: 20, l: 50 };
        const innerW = width - pad.l - pad.r;
        const innerH = height - pad.t - pad.b;

        const maxTotal = Math.max(...data.map(d => d.total), 1);
        const maxCum = Math.max(data[data.length - 1].cumulative, 1);

        const barW = innerW / currentChartDays;
        const barPad = currentChartDays <= 30 ? 2 : (currentChartDays <= 90 ? 1 : 0);
        const actualBarW = Math.max(1, barW - barPad);

        let barsHtml = '', lineD = '';
        data.forEach((d, i) => {
            const x = pad.l + i * barW + barPad / 2;
            let currentY = pad.t + innerH;
            const safeCounts = JSON.stringify(d.counts).replace(/"/g, '&quot;');
            barsHtml += `<g class="chart-bar-group" data-date="${d.date}" data-total="${d.total}" data-cum="${d.cumulative}" data-counts="${safeCounts}">`;

            for (const [source, val] of Object.entries(d.counts)) {
                if (val > 0) {
                    const h = (val / maxTotal) * innerH;
                    currentY -= h;
                    const color = SOURCE_COLORS[source] || SOURCE_COLORS.default;
                    barsHtml += `<rect x="${x}" y="${currentY}" width="${actualBarW}" height="${h}" fill="${color}" />`;
                }
            }
            barsHtml += `<rect x="${x}" y="${pad.t}" width="${barW}" height="${innerH}" fill="transparent" /></g>`;
            const cx = pad.l + i * barW + barW / 2;
            const cy = pad.t + innerH - (d.cumulative / maxCum) * innerH;
            lineD += i === 0 ? `M ${cx} ${cy} ` : `L ${cx} ${cy} `;
        });

        const lineHtml = `<path d="${lineD}" fill="none" stroke="var(--accent-color)" stroke-width="2" stroke-linejoin="round" />`;
        const labelsHtml = `
            <text x="${pad.l - 10}" y="${pad.t + 5}" fill="var(--text-dim)" font-size="10" text-anchor="end">${maxTotal}</text>
            <text x="${pad.l - 10}" y="${pad.t + innerH}" fill="var(--text-dim)" font-size="10" text-anchor="end">0</text>
            <text x="${pad.l + innerW + 10}" y="${pad.t + 5}" fill="var(--accent-color)" font-size="10" text-anchor="start">${maxCum}</text>
            <text x="${pad.l + innerW + 10}" y="${pad.t + innerH}" fill="var(--accent-color)" font-size="10" text-anchor="start">0</text>
        `;

        container.innerHTML = `<svg viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" style="width: 100%; height: 100%;"><line x1="${pad.l}" y1="${pad.t}" x2="${pad.l + innerW}" y2="${pad.t}" stroke="var(--glass-border)" stroke-dasharray="4" /><line x1="${pad.l}" y1="${pad.t + innerH / 2}" x2="${pad.l + innerW}" y2="${pad.t + innerH / 2}" stroke="var(--glass-border)" stroke-dasharray="4" /><line x1="${pad.l}" y1="${pad.t + innerH}" x2="${pad.l + innerW}" y2="${pad.t + innerH}" stroke="var(--glass-border)" />${labelsHtml}${barsHtml}${lineHtml}</svg><div class="chart-tooltip hidden" id="ankiTooltip"></div>`;

        const tooltip = document.getElementById('ankiTooltip');
        if (tooltip) {
            // 툴팁 찌그러짐 방지 및 마우스 간섭(깜빡임) 제거
            tooltip.style.pointerEvents = 'none';
            tooltip.style.whiteSpace = 'nowrap';
            tooltip.style.width = 'max-content';
            tooltip.style.position = 'absolute';
            tooltip.style.zIndex = '1000';
        }

        container.querySelectorAll('.chart-bar-group').forEach(group => {
            group.addEventListener('mouseenter', (e) => {
                const d = e.currentTarget.dataset;
                let countsHtml = '';
                try {
                    const countsMap = JSON.parse(d.counts || '{}');
                    for (const [s, v] of Object.entries(countsMap)) countsHtml += `<div style="display:flex; justify-content:space-between; gap:1.5rem; text-transform:capitalize;"><span>${s}</span> <strong>${v}</strong></div>`;
                } catch (e) { }
                tooltip.innerHTML = `<div style="font-weight:800; color:var(--accent-color); margin-bottom:0.4rem;">${d.date}</div>${countsHtml}<hr class="settings-divider" style="margin: 0.4rem 0;"><div style="display:flex; justify-content:space-between;"><span>Total</span> <strong>${d.total}</strong></div><div style="display:flex; justify-content:space-between; color:var(--accent-color);"><span>Cumulative</span> <strong>${d.cum}</strong></div>`;
                tooltip.classList.remove('hidden');
            });
            group.addEventListener('mousemove', (e) => {
                const containerRect = container.getBoundingClientRect();
                const tooltipWidth = tooltip.offsetWidth || 150; // 렌더링 지연 시 기본값 부여
                const tooltipHeight = tooltip.offsetHeight || 80;
                const margin = 15;

                // Position relative to container
                let left = e.clientX - containerRect.left;
                let top = e.clientY - containerRect.top;

                // Default: bottom-right. Flip if overflowing.
                let finalLeft = (left + margin + tooltipWidth > containerRect.width) ? (left - tooltipWidth - margin) : (left + margin);
                let finalTop = (top + margin + tooltipHeight > containerRect.height) ? (top - tooltipHeight - margin) : (top + margin);

                // Clamp to container edges to prevent ever going out of bounds
                finalLeft = Math.max(5, Math.min(finalLeft, containerRect.width - tooltipWidth - 5));
                finalTop = Math.max(5, Math.min(finalTop, containerRect.height - tooltipHeight - 5));

                tooltip.style.left = `${finalLeft}px`;
                tooltip.style.top = `${finalTop}px`;
            });
            group.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
        });
    },

    /**
     * Renders the list of available reports.
     * @param {Array} reports - List of report objects.
     * @param {number|string} activeId - The ID of the currently active report.
     */
    renderReportList(reports, activeId) {
        const container = document.getElementById('reportList');
        if (!container) return;

        if (!reports || reports.length === 0) {
            container.innerHTML = '<div class="u-text-dim" style="padding: 1rem; text-align: center;">No reports available.</div>';
            return;
        }

        container.innerHTML = reports.map(r => {
            const isActive = String(r.id) === String(activeId);
            return `
                <div class="c-report-item ${isActive ? 'c-report-item--active' : ''}" data-id="${r.id}">
                    <div class="c-report-item__info">
                        <span class="c-report-item__date">${escapeHTML(r.start_date)} ~ ${escapeHTML(r.end_date)}</span>
                    </div>
                    <button class="c-report-item__delete" data-id="${r.id}" title="Delete report">
                        <i class="icon-trash"></i>
                    </button>
                </div>
            `;
        }).join('');
    },

    /**
     * Renders the weekly AI report including Markdown summary and Network Graph.
     * @param {Object} report - Report data from the server.
     */
    renderReport(report) {
        const summaryContainer = document.getElementById('reportSummaryContent');
        const vizContainer = document.getElementById('reportVizChart');
        const warningContainer = document.getElementById('reportTruncationWarning');
        if (!summaryContainer || !vizContainer) return;

        // 0. Handle Truncation Warning
        if (warningContainer) {
            const isTruncated = !!(report && report.is_truncated);
            warningContainer.classList.toggle('u-hidden', !isTruncated);
            if (isTruncated) {
                warningContainer.innerHTML = `<div class="c-alert c-alert--warning">⚠️ <strong>토큰 한도 초과:</strong> 입력 데이터가 너무 많아 보고서의 일부 내용이 생략되었을 수 있습니다.</div>`;
            }
        }

        // 1. Render Markdown Summary with Multi-Language Support
        if (report) {
            const lang = state.currentLang || 'ko';
            let summary = null;
            let isFallback = false;

            // Why: Supports new map-based translation structure while maintaining backward compatibility with the 'report_summary' field as a default.
            // Why: Fallback properties added in case Go JSON serialization uses PascalCase
            const fallbackSummary = report.report_summary || report.ReportSummary || report.summary || '';
            
            if (report.translations && report.translations[lang]) {
                summary = report.translations[lang];
            } else {
                summary = fallbackSummary; 
                if (lang !== 'en') isFallback = true;
            }

            // Guard Clause: 데이터가 비어있을 경우 marked.parse 에러 방지
            if (!summary || typeof summary !== 'string') {
                summaryContainer.innerHTML = `<div class="u-text-dim" style="text-align: center; padding: 2rem;">선택하신 언어로 된 보고서 내용이 없습니다.</div>`;
            } else {
                // Now safely parsing guaranteed string
                const rawHtml = marked.parse(summary);
                
                // Why: Wrap sections in divs with specific classes to allow precise CSS targeting 
                // and branding (e.g., highlighting labels in Executive Summary vs. tables).
                const sections = [
                    { id: '1.', class: 'section-exec' },
                    { id: '2.', class: 'section-pending' },
                    { id: '3.', class: 'section-gap' },
                    { id: '4.', class: 'section-insights' }
                ];

                let processedHtml = '';
                
                // Add Fallback Warning if needed
                if (isFallback) {
                    const i18n = I18N_DATA[lang] || I18N_DATA['en'];
                    const warnMsg = lang === 'ko' ? '선택하신 언어의 번역본이 없어 영문 원본을 표시합니다.' : 'Translation not available. Showing default summary.';
                    processedHtml += `<div class="c-alert c-alert--info" style="margin-bottom: 1rem; font-size: 0.85rem;">ℹ️ ${warnMsg}</div>`;
                }

                const headerParts = rawHtml.split(/<h2/);
                if (headerParts.length > 1) {
                    processedHtml += headerParts[0]; // Content before first H2
                    headerParts.slice(1).forEach(part => {
                        const fullMatch = '<h2' + part;
                        let sectionClass = 'section-generic';
                        
                        for (const s of sections) {
                            if (fullMatch.includes(`h2>${s.id}`) || fullMatch.includes(`h2> ${s.id}`)) {
                                sectionClass = s.class;
                                break;
                            }
                        }
                        processedHtml += `<div class="${sectionClass}">${fullMatch}</div>`;
                    });
                } else {
                    processedHtml += rawHtml;
                }

                summaryContainer.innerHTML = processedHtml;
            }
        } else {
            summaryContainer.innerHTML = `<div class="u-text-dim" style="text-align: center; padding: 2rem;">생성된 보고서가 없습니다.</div>`;
        }

        // 2. Render ECharts Visualization
        if (report && report.visualization_data) {
            try {
                const data = JSON.parse(report.visualization_data);

                // Network Graph
                const networkContainer = document.getElementById('reportNetworkChart');
                if (networkContainer) {
                    this.renderNetworkGraph(networkContainer, data);
                }

                // Sankey Chart
                const sankeyContainer = document.getElementById('reportSankeyChart');
                if (sankeyContainer) {
                    this.renderSankeyChart(sankeyContainer, data);
                }
            } catch (e) {
                console.error("[Insights] Viz data parse error:", e);
                vizContainer.innerHTML = `<div class="u-text-dim" style="text-align: center; padding: 2rem;">시각화 데이터를 처리하지 못했습니다.</div>`;
            }
        } else {
            vizContainer.innerHTML = `<div class="u-text-dim" style="text-align: center; padding: 2rem;">관계망 데이터가 없습니다.</div>`;
        }
    },

    /**
     * Renders a relationship network graph using ECharts.
     * @param {HTMLElement} container - DOM element to render chart in.
     * @param {Object} data - Graph data {nodes: [], links: []}.
     */
    renderNetworkGraph(container, data) {
        if (!container || !data) return;

        let myChart = echarts.getInstanceByDom(container);
        if (!myChart) {
            myChart = echarts.init(container, state.currentTheme === 'dark' ? 'dark' : null);
        }

        const rawNodes = data.nodes || [];
        const uniqueNodesMap = new Map();
        rawNodes.forEach(n => {
            if (!uniqueNodesMap.has(n.id)) {
                uniqueNodesMap.set(n.id, n);
            }
        });
        const graphNodes = Array.from(uniqueNodesMap.values());
        // Why: Ensure all links from backend are rendered without modification as per Task 2.
        const graphLinks = data.links || data.edges || data.relations || [];

        // Resolve semantic colors using getCssVariableValue
        const accentColor = this.getCssVariableValue('--accent-color') || 'rgb(0, 242, 255)'; // User
        const primaryColor = this.getCssVariableValue('--color-primary') || 'rgb(59, 130, 246)'; // Internal
        const dimColor = this.getCssVariableValue('--text-dim') || 'rgb(156, 163, 175)'; // External (Gray)
        const textMain = this.getCssVariableValue('--text-main') || 'rgb(255, 255, 255)';

        const option = {
            backgroundColor: 'transparent',
            tooltip: {
                trigger: 'item',
                formatter: (params) => {
                    if (params.dataType === 'edge') {
                        const srcNode = graphNodes.find(n => n.id === params.data.source);
                        const tgtNode = graphNodes.find(n => n.id === params.data.target);
                        const srcName = srcNode ? srcNode.name : params.data.source;
                        const tgtName = tgtNode ? tgtNode.name : params.data.target;
                        return `${srcName} ↔ ${tgtName}<br/><b>소통량:</b> ${params.data.value}`;
                    }
                    const node = graphNodes.find(n => n.id === params.data.id);
                    const displayName = node ? node.name : params.data.id;
                    return `${displayName}: <b>${params.data.value || 0}</b>`;
                }
            },

            legend: [{
                data: ['User', 'Internal', 'External'],
                orient: 'vertical',
                right: 10,
                top: 20,
                textStyle: { color: dimColor }
            }],
            series: [{
                type: 'graph',
                layout: 'force',
                animation: true,
                draggable: true,
                edgeSymbol: ['none', 'arrow'],
                edgeSymbolSize: [4, 10],
                data: graphNodes.map(n => {
                    const cnt = n.value || 1;

                    let nodeColor = dimColor; // Default: External
                    let categoryName = 'External';

                    if (n.is_me) {
                        nodeColor = accentColor;
                        categoryName = 'User';
                    } else if (n.category === 'Internal') {
                        nodeColor = primaryColor;
                        categoryName = 'Internal';
                    }

                    return {
                        id: n.id,
                        name: n.name || n.id,
                        value: n.value,
                        symbolSize: Math.max(25, Math.min(100, cnt * 5)),
                        category: categoryName,
                        itemStyle: { color: nodeColor },
                        label: {
                            show: true,
                            fontSize: Math.max(10, Math.min(20, 10 + Math.log2(cnt + 1)))
                        }
                    };
                }),
                links: (() => {
                    return validateEdges(graphNodes, graphLinks).map(l => ({
                        source: l.source || l.from,
                        target: l.target || l.to,
                        value: l.weight || l.value || 1,
                        lineStyle: {
                            color: 'source',
                            width: Math.max(1, Math.min(10, l.weight || 1)),
                            opacity: 0.5,
                            curveness: 0.2
                        }
                    }));
                })(),
                categories: [{ name: 'User' }, { name: 'Internal' }, { name: 'External' }],
                roam: true,
                label: {
                    show: true,
                    position: 'right',
                    color: textMain,
                    fontSize: 10
                },
                force: {
                    repulsion: 2500, // 반발력을 높여 노드 간 간격 확보
                    gravity: 0.1,
                    edgeLength: [100, 250] // 선 길이를 조절하여 뭉침 방지
                },
                emphasis: {
                    focus: 'adjacency',
                    lineStyle: {
                        width: 10
                    }
                },
                blur: {
                    itemStyle: {
                        opacity: 0.1
                    },
                    lineStyle: {
                        opacity: 0.1,
                        width: 1
                    }
                }
            }]
        };

        myChart.setOption(option);

        // 기존 이벤트 리스너 제거 (중복 등록 방지)
        myChart.off('click');
        myChart.getZr().off('click');

        // 노드 클릭 시 상세 정보 팝업 표시
        const self = this;
        myChart.on('click', function (params) {
            if (params.dataType === 'node') {
                self._showNodePopup(params, container, data);
            } else {
                // 노드가 아닌 선 등을 클릭했을 때 팝업 숨기기
                self._hideNodePopup();
            }
        });

        // 그래프의 빈 배경 공간을 클릭했을 때 팝업 닫기
        myChart.getZr().on('click', function (event) {
            if (!event.target) {
                self._hideNodePopup();
            }
        });

        // Handle window resize
        if (!container.dataset.resizeBound) {
            window.addEventListener('resize', () => myChart.resize());
            container.dataset.resizeBound = "true";
        }
    },

    _hideNodePopup() {
        const popup = document.getElementById('nodeInfoPopup');
        if (popup) popup.style.display = 'none';
    },

    _showNodePopup(params, container, data) {
        let popup = document.getElementById('nodeInfoPopup');
        if (!popup) {
            popup = document.createElement('div');
            popup.id = 'nodeInfoPopup';
            popup.className = 'glass-card';
            popup.style.position = 'absolute';
            popup.style.zIndex = '1000';
            popup.style.padding = '1.25rem';
            popup.style.minWidth = '13.75rem';
            popup.style.boxShadow = '0 0.5rem 2rem rgba(0, 0, 0, 0.4)';
            popup.style.border = '0.0625rem solid var(--glass-border)';
            popup.style.borderRadius = '0.75rem';
            popup.style.backgroundColor = 'var(--bg-color)';

            if (getComputedStyle(container).position === 'static') {
                container.style.position = 'relative';
            }
            container.appendChild(popup);
        }

        const nodeId = params.data.id;
        const nodeName = params.data.name || nodeId;
        const messageCount = params.data.value || 0;

        const graphLinks = data.links || data.edges || data.relations || [];

        // 주요 소통 대상 상위 3명 추출 (ID 기반 필터링)
        const topLinks = graphLinks
            .filter(l => (l.source || l.from) === nodeId || (l.target || l.to) === nodeId)
            .sort((a, b) => (b.weight || b.value || 0) - (a.weight || a.value || 0))
            .slice(0, 3);

        let connectionsHtml = '';
        if (topLinks.length > 0) {
            connectionsHtml = '<div style="margin-top: 1rem; font-size: 0.85rem;">' +
                '<div style="color: var(--text-dim); margin-bottom: 0.5rem; font-weight: 600;">주요 소통 대상:</div>' +
                topLinks.map(l => {
                    const srcId = l.source || l.from;
                    const tgtId = l.target || l.to;
                    const otherId = srcId === nodeId ? tgtId : srcId;
                    const otherNode = graphNodes.find(n => n.id === otherId);
                    const otherName = otherNode ? otherNode.name : otherId;

                    return `<div style="display: flex; justify-content: space-between; margin-bottom: 0.3rem;">
                                <span style="color: var(--text-main);">${escapeHTML(otherName)}</span>
                                <span style="color: var(--accent-color); font-weight: 600;">${l.weight || l.value || 0}건</span>
                            </div>`;
                }).join('') +
                '</div>';
        }


        popup.innerHTML = `
            <div style="display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 0.5rem;">
                <h4 style="margin: 0; color: var(--text-main); font-size: 1.1rem; word-break: break-all;">${escapeHTML(nodeName)}</h4>
                <button id="closeNodePopup" style="background: none; border: none; color: var(--text-dim); cursor: pointer; font-size: 1.25rem; line-height: 1; padding: 0; margin-left: 1rem;">&times;</button>
            </div>
            <div style="font-size: 0.9rem; color: var(--text-dim);">
                총 소통량: <span style="color: var(--accent-color); font-weight: 800; font-size: 1rem;">${messageCount}</span>건
            </div>
            ${connectionsHtml}
        `;

        popup.style.display = 'block';

        document.getElementById('closeNodePopup').addEventListener('click', () => {
            popup.style.display = 'none';
        });

        const rect = container.getBoundingClientRect();
        let left = params.event.offsetX + 15;
        let top = params.event.offsetY + 15;

        const estWidth = 240;
        const estHeight = 150;
        if (left + estWidth > rect.width) left = params.event.offsetX - estWidth - 15;
        if (top + estHeight > rect.height) top = params.event.offsetY - estHeight - 15;

        popup.style.left = left + 'px';
        popup.style.top = top + 'px';
    },

    /**
     * @function renderSankeyChart
     * @description Renders a Sankey diagram showing communication flow.
     * @param {HTMLElement} container - The DOM element to render into.
     * @param {Object} data - The visualization data (nodes and links).
     */
    renderSankeyChart(container, data) {
        if (!container || !data || !data.nodes || !data.links) return;

        try {
            // 1. 메모리 누수 및 ECharts 초기화 경고 방지
            let myChart = echarts.getInstanceByDom(container);
            if (!myChart) {
                myChart = echarts.init(container, state.currentTheme === 'dark' ? 'dark' : null);
            }

            // Why: Enforce node deduplication using a Map to ensure unique IDs, as requested.
            // ECharts Sankey 매칭 에러 방지를 위해 Node ID를 완전히 소문자로 정규화
            const uniqueNodesMap = new Map();
            data.nodes.forEach(n => {
                const lowerId = (n.id || "").toLowerCase();
                if (!uniqueNodesMap.has(lowerId) && lowerId !== "") {
                    uniqueNodesMap.set(lowerId, { ...n, id: lowerId });
                }
            });
            const uniqueNodes = Array.from(uniqueNodesMap.values());

            // Link의 source/target도 소문자로 맞춘 후 유효성(validateEdges) 검사 진행
            const normalizedLinks = data.links.map(l => ({
                ...l,
                source: (l.source || l.from || "").toLowerCase(),
                target: (l.target || l.to || "").toLowerCase()
            }));

            // Why: Sankey diagrams in ECharts do not support cycles. We merge bidirectional edges into a DAG by sorting node names alphabetically.
            const mergedLinks = new Map();

            const validLinks = validateEdges(uniqueNodes, normalizedLinks);
            validLinks.forEach(l => {
                const src = l.source;
                const tgt = l.target;

                // Why: Enforce data integrity by skipping links with missing source or target nodes.
                if (!src || !tgt || src === tgt) return;

                // Sort alphabetically to create a unique key for the edge pair, effectively merging A->B and B->A into a single directed edge for DAG.
                const pair = [src, tgt].sort();
                const key = pair.join(':');

                const current = mergedLinks.get(key) || { source: pair[0], target: pair[1], value: 0 };
                current.value += (l.weight || l.value || 1);
                mergedLinks.set(key, current);
            });

            const links = Array.from(mergedLinks.values());

            // Resolve semantic colors using the utility
            const primaryColor = this.getCssVariableValue('--color-primary') || 'rgb(59, 130, 246)'; // Internal
            const accentColor = this.getCssVariableValue('--accent-color') || 'rgb(0, 242, 255)'; // User
            const dimColor = this.getCssVariableValue('--text-dim') || 'rgb(156, 163, 175)'; // External
            const textMain = this.getCssVariableValue('--text-main') || 'rgb(255, 255, 255)';

            const option = {
                tooltip: {
                    trigger: 'item',
                    triggerOn: 'mousemove',
                    formatter: params => {
                        if (params.dataType === 'edge') {
                            const srcNode = uniqueNodes.find(n => n.id === params.data.source);
                            const tgtNode = uniqueNodes.find(n => n.id === params.data.target);
                            const srcAlias = srcNode ? (srcNode.name || srcNode.id) : params.data.source;
                            const tgtAlias = tgtNode ? (tgtNode.name || tgtNode.id) : params.data.target;
                            return `${srcAlias} ↔ ${tgtAlias}: <b>${params.data.value}</b>`;
                        }
                        const node = uniqueNodes.find(n => n.id === params.data.id);
                        const displayName = node ? (node.name || node.id) : params.data.id;
                        return `${displayName}: <b>${params.value || 0}</b>`;
                    }

                },
                series: [{
                    type: 'sankey',
                    layout: 'none',
                    emphasis: { focus: 'adjacency' },
                    data: uniqueNodes.map(n => {
                        // Why: Dynamic color assignment based on Task 3 requirements.
                        let nodeColor = dimColor;
                        if (n.is_me) nodeColor = accentColor;
                        else if (n.category === 'Internal') nodeColor = primaryColor;

                        return {
                            name: n.id,
                            alias: n.name || n.id,
                            id: n.id,
                            itemStyle: { color: nodeColor }
                        };
                    }),
                    links: links,
                    lineStyle: {
                        color: 'gradient',
                        curveness: 0.5,
                        opacity: 0.3
                    },
                    label: {
                        color: textMain,
                        fontSize: 11,
                        fontFamily: 'Inter, sans-serif',
                        formatter: params => params.data.alias
                    }
                }]
            };

            myChart.setOption(option);
            window.addEventListener('resize', () => myChart && myChart.resize());
        } catch (err) {
            console.error('[INSIGHTS] Sankey Chart rendering failed:', err);
            container.innerHTML = `<div class="error-placeholder">Sankey error: ${err.message}</div>`;
        }
    },

    /**
     * @description Renders a loading spinner for JIT translation or other async tasks
     */
    renderLoading(container) {
        if (!container) return;
        container.innerHTML = `
            <div class="c-report-loading">
                <div class="spinner"></div>
                <p>AI 번역 생성 중...</p>
            </div>
        `;
    },

    /**
     * @description Renders an error message for failed translation or report fetching
     */
    renderError(container, message) {
        if (!container) return;
        container.innerHTML = `
            <div class="c-report-error">
                <div class="c-alert c-alert--error">
                    <strong>번역 실패:</strong> ${message || '알 수 없는 에러가 발생했습니다.'}<br>
                    <small>다시 한 번 언어를 선택해 주세요.</small>
                </div>
            </div>
        `;
    }
};