import { api } from './api.js';
import { state } from './state.js';
import { escapeHTML } from './utils.js';
import { I18N_DATA } from './locales.js';
import { calculateHeatmapLevel, calculateSourceDistribution, processTimeSeriesData } from './logic.js';

/**
 * @file insights.js
 * @description Professional Insights & Analytics module with Anki-style visualizations.
 */

// 동적 채널별 고유 색상 매핑 (새로운 채널이 추가되면 기본 색상이 부여됨)
const SOURCE_COLORS = {
    slack: '#E01E5A',
    whatsapp: '#25D366',
    gmail: '#EA4335',
    default: '#8b5cf6' // 세련된 보라빛 그레이 (Fallback)
};

export const insights = {
    lastStats: null,
    currentChartDays: 30,

    /**
     * Initializes the insights module.
     */
    init() {
        console.log("[Insights] Module Initialized");

        // Bind chart filters
        document.querySelectorAll('.chart-filters .filter-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                document.querySelectorAll('.chart-filters .filter-btn').forEach(b => b.classList.remove('active'));
                e.target.classList.add('active');
                this.currentChartDays = parseInt(e.target.dataset.days);
                this.renderAnkiChart();
            });
        });
    },

    /**
     * Called when the insights view is initialized or shown.
     */
    async onShow() {
        console.log("[Insights] View Shown");
        await this.refreshData();
    },

    /**
     * Refreshes stats and achievements data from the API and renders them.
     */
    async refreshData() {
        const loading = document.getElementById('loading');
        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        if (loading) {
            const p = loading.querySelector('p');
            if (p) p.textContent = "Loading insights data...";
            loading.classList.remove('hidden');
        }

        try {
            const [stats, allAch, userAch] = await Promise.all([
                api.fetchUserStats().catch(e => { console.error("[Insights] Stats failed:", e); return null; }),
                api.fetchAchievements().catch(e => { console.error("[Insights] All achievements failed:", e); return []; }),
                api.fetchUserAchievements().catch(e => { console.error("[Insights] User achievements failed:", e); return []; })
            ]);

            this.renderAll(stats, allAch, userAch);
        } catch (e) {
            console.error("[Insights] Unexpected error during refreshData", e);
        } finally {
            if (loading) {
                loading.classList.add('hidden');
                const p = loading.querySelector('p');
                if (p) p.textContent = i18n.loading || "Gemini is scanning for new tasks..."; // 기본 문구 복구
            }
        }
    },

    /**
     * Orchestrates the rendering of all insight components.
     * @param {Object} stats - User statistics data.
     * @param {Array} allAch - All possible achievements.
     * @param {Array} userAch - Achievements earned by the user.
     */
    renderAll(stats, allAch, userAch) {
        if (!stats) return;
        this.lastStats = stats;

        this.renderDailyGlance(stats);
        this.renderActivityHeatmap(stats);
        this.renderSourceDistribution(stats);
        this.renderWaitingMetrics(stats);
        this.renderHourlyActivity(stats);
        this.renderAchievements(allAch, userAch, stats);
        this.renderAnkiChart();
    },

    /**
     * Renders the 'Daily Glance' summary text with a personal touch.
     */
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

    /**
     * Renders the activity heatmap for the last 30 days.
     */
    renderActivityHeatmap(stats) {
        const container = document.getElementById('activityHeatmap');
        if (!container) return;
        container.style.position = 'relative'; // 툴팁 위치 계산을 위해 추가

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        let html = '<div class="heatmap-grid">';
        const today = new Date();

        // Render 30 days
        for (let i = 29; i >= 0; i--) {
            const d = new Date(today);
            d.setDate(d.getDate() - i);
            const dateStr = d.toISOString().split('T')[0];

            const taskCount = stats.daily_completions[dateStr] || 0;
            const level = calculateHeatmapLevel(taskCount);

            const tooltipText = (i18n.heatmapTaskTooltip || "{count} tasks completed ({date})")
                .replace('{count}', taskCount)
                .replace('{date}', dateStr);

            // 브라우저 기본 title 대신 data-tooltip 사용
            html += `<div class="heatmap-day" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
        }

        html += '</div>';
        html += '<div class="chart-tooltip hidden" id="dailyHeatmapTooltip"></div>';
        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'dailyHeatmapTooltip');
    },

    /**
     * Renders the channel distribution chart using a sleek stacked bar.
     */
    renderSourceDistribution(stats) {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;

        const dist = calculateSourceDistribution(stats.source_distribution);

        if (Object.keys(dist).length === 0) {
            container.innerHTML = '<p class="empty-msg">No channel data available.</p>';
            return;
        }

        let barsHtml = '';
        let legendHtml = '';
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

    /**
     * Renders waiting metrics to highlight bottlenecks.
     */
    renderWaitingMetrics(stats) {
        const container = document.getElementById('waitingMetrics');
        if (!container) return;

        // 상위 카드를 찾아 눈에 띄는 하이라이트 클래스를 부여합니다.
        const card = container.closest('.insights-card');
        if (card) card.classList.add('highlight-card');

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        // 배경색이 카드에 칠해지므로, 내부 아이템의 테두리와 배경은 투명하게 없애 깔끔하게 만듭니다.
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

    /**
     * Renders hourly activity distribution (Anki-style hourly breakdown).
     */
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

        // Get max value for normalization
        let max = 0;
        for (let h = 0; h < 24; h++) {
            if (stats.hourly_activity[h] > max) max = stats.hourly_activity[h];
        }

        let html = '<div class="heatmap-grid" style="grid-template-columns: repeat(12, 1fr);">';
        for (let h = 0; h < 24; h++) {
            const count = stats.hourly_activity[h] || 0;
            // Level 0-4
            const level = max > 0 ? Math.ceil((count / max) * 4) : 0;

            const timeStr = `${h}:00`;
            const tooltipText = (i18n.hourlyTaskTooltip || "{count} tasks completed ({time})")
                .replace('{count}', count)
                .replace('{time}', timeStr);

            html += `<div class="heatmap-day" data-level="${level}" data-tooltip="${tooltipText}"></div>`;
        }
        html += '</div>';
        html += '<div class="chart-tooltip hidden" id="hourlyHeatmapTooltip"></div>';
        container.innerHTML = html;
        this.bindHeatmapTooltip(container, 'hourlyHeatmapTooltip');
    },

    /**
     * 히트맵용 커스텀 툴팁 이벤트를 바인딩합니다.
     */
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

                if (leftPos + tooltip.offsetWidth > rect.width) {
                    leftPos = (e.clientX - rect.left) - tooltip.offsetWidth - 15;
                }
                if (topPos + tooltip.offsetHeight > rect.height) {
                    topPos = (e.clientY - rect.top) - tooltip.offsetHeight - 15;
                }

                tooltip.style.left = leftPos + 'px';
                tooltip.style.top = topPos + 'px';
            });
            day.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
        });
    },

    /**
     * Renders achievements with progress bars and unlocked status.
     */
    renderAchievements(allAch, userAch, stats) {
        const container = document.getElementById('achievementsList');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        // API 응답이 { achievements: [...] } 형태일 수 있으므로 배열 추출
        const allList = Array.isArray(allAch) ? allAch : (allAch?.achievements || allAch?.data || []);
        const userList = Array.isArray(userAch) ? userAch : (userAch?.achievements || userAch?.data || []);

        if (!allList || allList.length === 0) {
            container.innerHTML = '<p class="empty-msg">No milestones found.</p>';
            return;
        }

        const userAchIds = new Set(userList.map(ua => ua.achievement_id));

        container.innerHTML = allList.map(ach => {
            const isUnlocked = userAchIds.has(ach.id);

            let progress = 0;
            if (isUnlocked) {
                progress = ach.target_value;
            } else if (ach.criteria_type === 'total_tasks') {
                progress = stats ? (stats.total_completed || 0) : 0;
            } else if (ach.criteria_type === 'level') {
                progress = state.userProfile ? (state.userProfile.level || 1) : 1;
            }

            const percent = Math.min(100, Math.round((progress / ach.target_value) * 100));

            return `
                <div class="achievement-card ${isUnlocked ? 'unlocked' : 'locked'}">
                    <div class="achievement-icon">${ach.icon}</div>
                    <div class="achievement-info">
                        <div class="achievement-header">
                            <span class="achievement-name">${escapeHTML(ach.name)}</span>
                            ${isUnlocked ? `<span class="status-badge">${i18n.unlocked}</span>` : ''}
                        </div>
                        <p class="achievement-desc">${escapeHTML(ach.description)}</p>
                        <div class="achievement-progress-bar">
                            <div class="progress-fill" style="width: ${percent}%"></div>
                        </div>
                        <div class="achievement-footer">
                            <span class="xp-reward">+${ach.xp_reward || 0} XP</span>
                            <span class="progress-text">${Math.min(progress, ach.target_value)}/${ach.target_value}</span>
                        </div>
                    </div>
                </div>
            `;
        }).join('');
    },

    /**
     * Renders the Anki-style completion trend chart using zero-dependency SVG.
     */
    renderAnkiChart() {
        const stats = this.lastStats;
        if (!stats) return;

        const container = document.getElementById('ankiChartContainer');
        if (!container) return;

        // Backend will need to provide completion_history
        const history = stats.completion_history || [];
        const data = processTimeSeriesData(history, this.currentChartDays);

        const width = 800;
        const height = 240;
        const pad = { t: 20, r: 40, b: 20, l: 40 };
        const innerW = width - pad.l - pad.r;
        const innerH = height - pad.t - pad.b;

        const maxTotal = Math.max(...data.map(d => d.total), 1);
        const maxCum = Math.max(data[data.length - 1].cumulative, 1);

        const barW = innerW / this.currentChartDays;
        const barPad = this.currentChartDays <= 30 ? 2 : (this.currentChartDays <= 90 ? 1 : 0);
        const actualBarW = Math.max(1, barW - barPad);

        let barsHtml = '';
        let lineD = '';

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

        container.innerHTML = `
            <svg viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" style="width: 100%; height: 100%; overflow: visible;">
                <line x1="${pad.l}" y1="${pad.t}" x2="${pad.l + innerW}" y2="${pad.t}" stroke="var(--glass-border)" stroke-dasharray="4" />
                <line x1="${pad.l}" y1="${pad.t + innerH / 2}" x2="${pad.l + innerW}" y2="${pad.t + innerH / 2}" stroke="var(--glass-border)" stroke-dasharray="4" />
                <line x1="${pad.l}" y1="${pad.t + innerH}" x2="${pad.l + innerW}" y2="${pad.t + innerH}" stroke="var(--glass-border)" />
                ${labelsHtml}${barsHtml}${lineHtml}
            </svg>
            <div class="chart-tooltip hidden" id="ankiTooltip"></div>
        `;

        // Tooltip logic
        const tooltip = document.getElementById('ankiTooltip');
        container.querySelectorAll('.chart-bar-group').forEach(group => {
            group.addEventListener('mouseenter', (e) => {
                const d = e.currentTarget.dataset;
                let countsHtml = '';
                try {
                    const countsMap = JSON.parse(d.counts || '{}');
                    for (const [source, val] of Object.entries(countsMap)) {
                        countsHtml += `<div style="display:flex; justify-content:space-between; gap:1.5rem; text-transform:capitalize;"><span>${source}</span> <strong>${val}</strong></div>`;
                    }
                } catch (e) { }

                tooltip.innerHTML = `<div style="font-weight:800; color:var(--accent-color); margin-bottom:0.4rem;">${d.date}</div>${countsHtml}<hr class="settings-divider" style="margin: 0.4rem 0;"><div style="display:flex; justify-content:space-between;"><span>Total</span> <strong>${d.total}</strong></div><div style="display:flex; justify-content:space-between; color:var(--accent-color);"><span>Cumulative</span> <strong>${d.cum}</strong></div>`;
                tooltip.classList.remove('hidden');
            });
            group.addEventListener('mousemove', (e) => {
                const rect = container.getBoundingClientRect();
                let leftPos = e.clientX - rect.left + 15;
                let topPos = e.clientY - rect.top + 15;

                // 화면(컨테이너) 오른쪽을 넘어가면 마우스 왼쪽으로 툴팁 반전
                if (leftPos + tooltip.offsetWidth > rect.width) {
                    leftPos = (e.clientX - rect.left) - tooltip.offsetWidth - 15;
                }
                // 화면(컨테이너) 아래쪽을 넘어가면 마우스 위쪽으로 툴팁 반전
                if (topPos + tooltip.offsetHeight > rect.height) {
                    topPos = (e.clientY - rect.top) - tooltip.offsetHeight - 15;
                }

                tooltip.style.left = leftPos + 'px';
                tooltip.style.top = topPos + 'px';
            });
            group.addEventListener('mouseleave', () => tooltip.classList.add('hidden'));
        });
    }
};