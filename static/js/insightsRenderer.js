import { state } from './state.js';
import { escapeHTML } from './utils.js';
import { I18N_DATA } from './locales.js';
import { calculateHeatmapLevel, calculateSourceDistribution, processTimeSeriesData } from './logic.js';

/**
 * @file insightsRenderer.js
 * @description Handles all DOM rendering and visualizations for the Insights module.
 */

// 동적 채널별 고유 색상 매핑
const SOURCE_COLORS = {
    slack: '#E01E5A',
    whatsapp: '#25D366',
    gmail: '#EA4335',
    default: '#8b5cf6'
};

export const insightsRenderer = {
    renderDailyGlance(stats) {
        const container = document.getElementById('dailyGlance');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        let html = '<p>';
        const totalHtml = `<span class="accent">${stats.total_completed}</span>`;
        html += (i18n.glanceTotalCompleted || "Total completed: {count}. ").replace('{count}', totalHtml);

        if (stats.peak_time && stats.peak_time !== "-") {
            const peakHtml = `<span class="accent">${stats.peak_time}</span>`;
            html += (i18n.glancePeakTime || "Peak focus time: {time}. ").replace('{time}', peakHtml);
        }

        if (stats.abandoned_tasks > 0) {
            const abandonedText = (i18n.glanceAbandoned || '⚠️ {count} items have been pending...').replace('{count}', `<span style="color:#ff3b30; font-weight:800;">${stats.abandoned_tasks}</span>`);
            html += `<br>${abandonedText}</p>`;
        } else {
            const clearText = i18n.glanceAllClear || '✨ All caught up! No stale tasks found.';
            html += `<br><span style="font-weight:600;">${clearText}</span></p>`;
        }
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

        let html = '<div style="display: flex; flex-direction: column; gap: 8px; margin-top: 0.5rem;">';

        // 상단 요일 라벨 표시
        const daysOfWeek = lang === 'ko'
            ? ['일', '월', '화', '수', '목', '금', '토']
            : ['S', 'M', 'T', 'W', 'T', 'F', 'S'];

        let xLabels = '<div style="display: grid; grid-template-columns: repeat(7, 1fr); gap: 6px; font-size: 0.75rem; color: var(--text-dim); text-align: center; font-weight: 700;">';
        daysOfWeek.forEach(d => { xLabels += `<div>${d}</div>`; });
        xLabels += '</div>';

        html += xLabels;

        let gridHtml = '<div class="heatmap-grid" style="grid-template-columns: repeat(7, 1fr); gap: 6px;">';

        // 시작 요일에 맞추어 앞부분 빈칸 채우기
        const startDay = startDate.getDay();
        for (let i = 0; i < startDay; i++) {
            gridHtml += `<div class="heatmap-day" style="background: transparent; box-shadow: none; cursor: default;"></div>`;
        }

        for (let i = 29; i >= 0; i--) {
            const d = new Date(today);
            d.setDate(d.getDate() - i);
            const dateStr = d.toISOString().split('T')[0];

            const taskCount = stats.daily_completions[dateStr] || 0;
            const level = calculateHeatmapLevel(taskCount);
            const tooltipText = (i18n.heatmapTaskTooltip || "{count} tasks completed ({date})")
                .replace('{count}', taskCount).replace('{date}', dateStr);

            // 오늘 날짜 강조 클래스 추가
            const isToday = i === 0;
            const extraClass = isToday ? ' today-highlight' : '';

            gridHtml += `<div class="heatmap-day${extraClass}" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
        }

        gridHtml += '</div>';
        html += gridHtml + '</div><div class="chart-tooltip hidden" id="dailyHeatmapTooltip"></div>';

        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'dailyHeatmapTooltip');
    },

    renderSourceDistribution(stats) {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;

        const dist = calculateSourceDistribution(stats.source_distribution);
        if (Object.keys(dist).length === 0) {
            container.innerHTML = '<p class="empty-msg">No channel data available.</p>';
            return;
        }

        let barsHtml = '', legendHtml = '';
        for (const [source, percentage] of Object.entries(dist)) {
            if (percentage > 0) {
                const color = SOURCE_COLORS[source] || SOURCE_COLORS.default;
                barsHtml += `<div class="stacked-bar-segment" style="width: ${percentage}%; background-color: ${color};"></div>`;
                legendHtml += `<span style="color: ${color}; font-weight: 600; text-transform: capitalize;">${source} (${percentage}%)</span>`;
            }
        }

        container.innerHTML = `
            <div class="stacked-bar-container">${barsHtml}</div>
            <div class="distribution-legend" style="display:flex; gap:1.2rem; flex-wrap:wrap; justify-content:center; margin-top:1rem; font-size:0.85rem;">${legendHtml}</div>
        `;
    },

    renderWaitingMetrics(stats) {
        const container = document.getElementById('waitingMetrics');
        if (!container) return;

        const card = container.closest('.insights-card');
        if (card) card.classList.add('highlight-card');

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        container.innerHTML = `
            <div class="metric-item" style="border:none; background:transparent; padding:0;">
                <span class="label" style="display:block; font-size:0.9rem; color:var(--text-dim); margin-bottom:0.5rem;">${i18n.pendingMeTasks || 'My Pending Tasks'}</span>
                <span class="value" style="font-size:2.5rem; font-weight:800; color:var(--text-main);">${stats.pending_me || 0}</span>
            </div>
            <div class="metric-item" style="border:none; background:transparent; padding:0;">
                <span class="label" style="display:block; font-size:0.9rem; color:var(--text-dim); margin-bottom:0.5rem;">${i18n.needsAttentionTasks || 'Needs Attention'}</span>
                <span class="value" style="font-size:2.5rem; font-weight:800; color:#ff3b30;">${stats.abandoned_tasks || 0}</span>
            </div>
        `;
    },

    renderHourlyActivity(stats) {
        const container = document.getElementById('hourlyActivity');
        if (!container) return;
        if (!stats.hourly_activity || Object.keys(stats.hourly_activity).length === 0) {
            container.innerHTML = '<p class="empty-msg">Waiting for more completion data...</p>';
            return;
        }
        container.style.position = 'relative';

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        let max = 0;
        for (let h = 0; h < 24; h++) if (stats.hourly_activity[h] > max) max = stats.hourly_activity[h];

        let html = '<div class="hourly-layout" style="display: flex; flex-direction: column; gap: 6px;">';
        const renderRow = (startHour, endHour, label) => {
            let rowHtml = `<div style="display: flex; align-items: center; gap: 8px;">`;
            rowHtml += `<span style="font-size: 0.7rem; color: var(--text-dim); width: 1.5rem; text-align: right; font-weight: 700;">${label}</span>`;
            rowHtml += '<div class="heatmap-grid" style="grid-template-columns: repeat(12, 1fr); flex: 1; gap: 4px;">';

            for (let h = startHour; h < endHour; h++) {
                const count = stats.hourly_activity[h] || 0;
                const level = max > 0 ? Math.ceil((count / max) * 4) : 0;
                const tooltipText = (i18n.hourlyTaskTooltip || "{count} tasks completed ({time})").replace('{count}', count).replace('{time}', `${h}:00`);
                rowHtml += `<div class="heatmap-day" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
            }
            return rowHtml + '</div></div>';
        };

        html += renderRow(0, 12, 'AM') + renderRow(12, 24, 'PM');
        html += '<div style="display: flex; align-items: center; gap: 8px; margin-top: 2px;"><span style="width: 1.5rem;"></span>';
        html += '<div style="display: grid; grid-template-columns: repeat(12, 1fr); flex: 1; gap: 4px; font-size: 0.65rem; color: var(--text-dim); text-align: center;">';
        for (let i = 0; i < 12; i++) {
            html += `<div>${i === 0 ? '12' : i % 3 === 0 ? i : ''}</div>`;
        }
        html += '</div></div></div><div class="chart-tooltip hidden" id="hourlyHeatmapTooltip"></div>';

        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'hourlyHeatmapTooltip');
    },

    bindHeatmapTooltip(container, tooltipId) {
        const tooltip = container.querySelector('#' + tooltipId);
        if (!tooltip) return;
        container.querySelectorAll('.heatmap-day').forEach(day => {
            day.addEventListener('mouseenter', (e) => {
                tooltip.innerHTML = `<div style="font-weight:600;">${e.currentTarget.dataset.tooltip}</div>`;
                tooltip.classList.remove('hidden');
            });
            day.addEventListener('mousemove', (e) => {
                const rect = container.getBoundingClientRect();
                let leftPos = e.clientX - rect.left + 15;
                let topPos = e.clientY - rect.top + 15;
                if (leftPos + tooltip.offsetWidth > rect.width) leftPos = e.clientX - rect.left - tooltip.offsetWidth - 15;
                if (topPos + tooltip.offsetHeight > rect.height) topPos = e.clientY - rect.top - tooltip.offsetHeight - 15;
                tooltip.style.left = leftPos + 'px';
                tooltip.style.top = topPos + 'px';
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
        container.innerHTML = allList.map(ach => {
            const isUnlocked = userAchIds.has(ach.id);
            let progress = isUnlocked ? ach.target_value : (ach.criteria_type === 'total_tasks' ? (stats?.total_completed || 0) : (state.userProfile?.level || 1));
            const percent = Math.min(100, Math.round((progress / ach.target_value) * 100));

            const localizedName = i18n.achievements?.[ach.name]?.name || ach.name;
            const localizedDesc = i18n.achievements?.[ach.name]?.desc || ach.description;

            return `
                <div class="achievement-card ${isUnlocked ? 'unlocked' : 'locked'}">
                    <div class="achievement-icon">${ach.icon}</div>
                    <div class="achievement-info">
                        <div class="achievement-header">
                            <span class="achievement-name">${escapeHTML(localizedName)}</span>
                            ${isUnlocked ? `<span class="status-badge">${i18n.unlocked}</span>` : ''}
                        </div>
                        <p class="achievement-desc">${escapeHTML(localizedDesc)}</p>
                        <div class="achievement-progress-bar"><div class="progress-fill" style="width: ${percent}%"></div></div>
                        <div class="achievement-footer">
                            <span class="xp-reward">+${ach.xp_reward || 0} XP</span>
                            <span class="progress-text">${Math.min(progress, ach.target_value)}/${ach.target_value}</span>
                        </div>
                    </div>
                </div>`;
        }).join('');
    },

    renderAnkiChart(stats, currentChartDays) {
        if (!stats) return;
        const container = document.getElementById('ankiChartContainer');
        if (!container) return;

        const history = stats.completion_history || [];
        const data = processTimeSeriesData(history, currentChartDays);

        const width = 800, height = 240;
        const pad = { t: 20, r: 40, b: 20, l: 40 };
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

        container.innerHTML = `<svg viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" style="width: 100%; height: 100%; overflow: visible;"><line x1="${pad.l}" y1="${pad.t}" x2="${pad.l + innerW}" y2="${pad.t}" stroke="var(--glass-border)" stroke-dasharray="4" /><line x1="${pad.l}" y1="${pad.t + innerH / 2}" x2="${pad.l + innerW}" y2="${pad.t + innerH / 2}" stroke="var(--glass-border)" stroke-dasharray="4" /><line x1="${pad.l}" y1="${pad.t + innerH}" x2="${pad.l + innerW}" y2="${pad.t + innerH}" stroke="var(--glass-border)" />${labelsHtml}${barsHtml}${lineHtml}</svg><div class="chart-tooltip hidden" id="ankiTooltip"></div>`;

        const tooltip = document.getElementById('ankiTooltip');
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
                const rect = container.getBoundingClientRect();
                let leftPos = e.clientX - rect.left + 15, topPos = e.clientY - rect.top + 15;
                if (leftPos + tooltip.offsetWidth > rect.width) leftPos = e.clientX - rect.left - tooltip.offsetWidth - 15;
                if (topPos + tooltip.offsetHeight > rect.height) topPos = e.clientY - rect.top - tooltip.offsetHeight - 15;
                tooltip.style.left = leftPos + 'px'; tooltip.style.top = topPos + 'px';
            });
            group.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
        });
    }
};