import { api } from './api.js';
import { state } from './state.js';
import { escapeHTML } from './utils.js';
import { I18N_DATA } from './locales.js';
import { calculateHeatmapLevel, calculateSourceDistribution } from './logic.js';

/**
 * @file insights.js
 * @description Professional Insights & Analytics module with Anki-style visualizations.
 */

export const insights = {
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
        try {
            const [stats, allAch, userAch] = await Promise.all([
                api.fetchUserStats(),
                api.fetchAchievements(),
                api.fetchUserAchievements()
            ]);
            
            this.renderAll(stats, allAch, userAch);
        } catch (e) {
            console.error("[Insights] Failed to load statistics data", e);
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

        this.renderDailyGlance(stats);
        this.renderActivityHeatmap(stats);
        this.renderSourceDistribution(stats);
        this.renderWaitingMetrics(stats);
        this.renderHourlyActivity(stats);
        this.renderAchievements(allAch, userAch, stats);
    },

    /**
     * Renders the 'Daily Glance' summary text with a personal touch.
     */
    renderDailyGlance(stats) {
        const container = document.getElementById('dailyGlance');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        let html = `<p>${i18n.totalCompleted}: <span class="accent">${stats.total_completed}</span>. `;
        
        if (stats.peak_time && stats.peak_time !== "-") {
            html += `${i18n.peakFocusTime}: <span class="accent">${stats.peak_time}</span>. `;
        }
        
        if (stats.abandoned_tasks > 0) {
            html += `<br><span style="color:#ff3b30; font-weight:800;">⚠️ ${stats.abandoned_tasks}</span> items have been pending for over 3 days. Time to clear them up!`;
        } else {
            html += `<br>✨ All caught up! No stale tasks found. Keep it up!`;
        }

        container.innerHTML = html;
    },

    /**
     * Renders the activity heatmap for the last 30 days.
     */
    renderActivityHeatmap(stats) {
        const container = document.getElementById('activityHeatmap');
        if (!container) return;

        let html = '<div class="heatmap-grid">';
        const today = new Date();
        
        // Render 30 days
        for (let i = 29; i >= 0; i--) {
            const d = new Date(today);
            d.setDate(d.getDate() - i);
            const dateStr = d.toISOString().split('T')[0];
            
            const taskCount = stats.daily_completions[dateStr] || 0;
            const level = calculateHeatmapLevel(taskCount);
            
            html += `<div class="heatmap-day" data-level="${level}" title="${taskCount} tasks (${dateStr})"></div>`;
        }
        
        html += '</div>';
        container.innerHTML = html;
    },

    /**
     * Renders the channel distribution chart using a sleek stacked bar.
     */
    renderSourceDistribution(stats) {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;

        const dist = calculateSourceDistribution(stats.source_distribution);
        
        if (dist.slack === 0 && dist.whatsapp === 0 && dist.gmail === 0) {
            container.innerHTML = '<p class="empty-msg">No channel data available.</p>';
            return;
        }

        container.innerHTML = `
            <div class="stacked-bar-container">
                ${dist.slack > 0 ? `<div class="stacked-bar-segment slack" style="width: ${dist.slack}%"></div>` : ''}
                ${dist.whatsapp > 0 ? `<div class="stacked-bar-segment whatsapp" style="width: ${dist.whatsapp}%"></div>` : ''}
                ${dist.gmail > 0 ? `<div class="stacked-bar-segment gmail" style="width: ${dist.gmail}%"></div>` : ''}
            </div>
            <div class="distribution-legend">
                ${dist.slack > 0 ? `<span class="slack">Slack (${dist.slack}%)</span>` : ''}
                ${dist.whatsapp > 0 ? `<span class="whatsapp">WhatsApp (${dist.whatsapp}%)</span>` : ''}
                ${dist.gmail > 0 ? `<span class="gmail">Gmail (${dist.gmail}%)</span>` : ''}
            </div>
        `;
    },

    /**
     * Renders waiting metrics to highlight bottlenecks.
     */
    renderWaitingMetrics(stats) {
        const container = document.getElementById('waitingMetrics');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        container.innerHTML = `
            <div class="metric-item">
                <span class="label">${i18n.waitingOnMe || 'Waiting on Me'}</span>
                <span class="value">${stats.pending_me || 0}</span>
            </div>
            <div class="metric-item">
                <span class="label">${i18n.waitingOnOthers || 'Waiting on Others'}</span>
                <span class="value">${stats.abandoned_tasks || 0}</span>
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
            html += `<div class="heatmap-day" data-level="${level}" title="${count} tasks at ${h}:00"></div>`;
        }
        html += '</div>';
        container.innerHTML = html;
    },

    /**
     * Renders achievements with progress bars and unlocked status.
     */
    renderAchievements(allAch, userAch, stats) {
        const container = document.getElementById('achievementsList');
        if (!container) return;

        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];

        if (!allAch || allAch.length === 0) {
            container.innerHTML = '<p class="empty-msg">No milestones found.</p>';
            return;
        }

        const userAchIds = new Set(userAch.map(ua => ua.achievement_id));

        container.innerHTML = allAch.map(ach => {
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
    }
};