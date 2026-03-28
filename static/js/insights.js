import { api } from './api.js';
import { state } from './state.js';
import { I18N_DATA } from './locales.js';
import { insightsRenderer } from './insightsRenderer.js';

/**
 * @file insights.js
 * @description Controller for Insights & Analytics module. Delegates rendering to insightsRenderer.js.
 */

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

                if (this.lastStats) {
                    insightsRenderer.renderAnkiChart(this.lastStats, this.currentChartDays);
                }
            });
        });

        // 2단계 탭 바인딩 (통계 / 보고서)
        const statsTab = document.querySelector('[data-tab="insightsStatsTab"]');
        const reportsTab = document.querySelector('[data-tab="insightsReportsTab"]');
        
        // Panels
        const statsPanel = document.getElementById('insightsStatsTab');
        const reportsPanel = document.getElementById('insightsReportsTab');

        if (statsTab && reportsTab && statsPanel && reportsPanel) {
            statsTab.addEventListener('click', () => {
                statsTab.classList.add('active');
                reportsTab.classList.remove('active');
                statsPanel.classList.add('c-tabs__panel--active');
                reportsPanel.classList.remove('c-tabs__panel--active');
            });

            reportsTab.addEventListener('click', async () => {
                reportsTab.classList.add('active');
                statsTab.classList.remove('active');
                reportsPanel.classList.add('c-tabs__panel--active');
                statsPanel.classList.remove('c-tabs__panel--active');
                
                // Fetch report data if not already loaded or on refresh
                await this.refreshReport();
            });
        }
    },

    /**
     * Fetches and renders the weekly AI report.
     */
    async refreshReport() {
        try {
            const report = await api.getInsightReport();
            insightsRenderer.renderReport(report);
        } catch (e) {
            console.error("[Insights] Report fetch failed:", e);
            const summaryContainer = document.getElementById('reportSummaryContent');
            if (summaryContainer) {
                summaryContainer.innerHTML = `<div class="u-text-dim" style="text-align: center; padding: 2rem; color: var(--color-error);">Report generation failed: ${e.message}</div>`;
            }
        }
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
            loading.classList.add('active');
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
                loading.classList.remove('active');
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

        insightsRenderer.renderDailyGlance(stats);
        insightsRenderer.renderActivityHeatmap(stats);
        insightsRenderer.renderSourceDistribution(stats);
        insightsRenderer.renderWaitingMetrics(stats);
        insightsRenderer.renderHourlyActivity(stats);
        insightsRenderer.renderAchievements(allAch, userAch, stats);
        insightsRenderer.renderAnkiChart(stats, this.currentChartDays);
    }
};