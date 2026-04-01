import { api } from './api.js';
import { state } from './state.ts';
import { I18N_DATA } from './locales.js';
import { insightsRenderer } from './insightsRenderer.js';
import { events, EVENTS } from './events.js';
import { TokenUsageCard } from './components/token-usage';

/**
 * @file insights.js
 * @description Controller for Insights & Analytics module. Delegates rendering to insightsRenderer.js.
 */

export const insights = {
    lastStats: null,
    lastReport: null,
    currentChartDays: 30,
    tokenCard: null,

    /**
     * Initializes the insights module.
     */
    init() {
        console.log("[Insights] Module Initialized");
        this.tokenCard = new TokenUsageCard('tokenUsageContainer');

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

        // Why: Bind secondary tabs (Stats/Reports) using a specific class to avoid conflicts
        // with the global tab handler which controls primary navigation.
        const statsTab = document.querySelector('.insights-tab-btn[data-tab="insightsStatsTab"]');
        const reportsTab = document.querySelector('.insights-tab-btn[data-tab="insightsReportsTab"]');
        const insightsTabBtns = [statsTab, reportsTab];

        // Panels
        const statsPanel = document.getElementById('insightsStatsTab');
        const reportsPanel = document.getElementById('insightsReportsTab');

        if (statsTab && reportsTab && statsPanel && reportsPanel) {
            statsTab.addEventListener('click', () => {
                insightsTabBtns.forEach(btn => btn.classList.remove('active'));
                statsTab.classList.add('active');

                statsPanel.classList.add('c-tabs__panel--active');
                reportsPanel.classList.remove('c-tabs__panel--active');
            });

            reportsTab.addEventListener('click', async () => {
                insightsTabBtns.forEach(btn => btn.classList.remove('active'));
                reportsTab.classList.add('active');

                reportsPanel.classList.add('c-tabs__panel--active');
                statsPanel.classList.remove('c-tabs__panel--active');

                // Fetch report data if not already loaded or on refresh
                await this.refreshReport();
            });
        }

        this.bindReportEvents();
        this.initDatePickers();

        // Why: Re-render charts and reports instantly on theme/language changes
        // without re-fetching data from the server, providing a smoother UX.
        events.on(EVENTS.THEME_CHANGED, () => {
            if (!document.getElementById('insightsSection')?.classList.contains('hidden')) {
                if (this.lastStats) {
                    insightsRenderer.renderAnkiChart(this.lastStats, this.currentChartDays);
                }
                if (this.lastReport) {
                    insightsRenderer.renderReport(this.lastReport);
                }
            }
        });

        events.on(EVENTS.LANGUAGE_CHANGED, async (payload) => {
            const langCode = (typeof payload === 'object') ? payload.langCode : payload;
            console.log(`[Insights] Language changed to: ${langCode}`);

            if (!document.getElementById('insightsSection')?.classList.contains('hidden')) {
                if (this.lastReport) {
                    // Why: If translation is missing for the selected language, trigger JIT translation via AI.
                    if (langCode !== 'en' && (!this.lastReport.translations || !this.lastReport.translations[langCode])) {
                        const content = document.getElementById('reportSummaryContent');
                        insightsRenderer.renderLoading(content);

                        try {
                            const result = await api.translateReport(this.lastReport.id, langCode);
                            if (result && result.summary) {
                                // Update local cache to avoid re-fetching
                                if (!this.lastReport.translations) this.lastReport.translations = {};
                                this.lastReport.translations[langCode] = result.summary;
                                insightsRenderer.renderReport(this.lastReport);
                            }
                        } catch (err) {
                            console.error("[Insights] JIT Translation failed:", err);
                            insightsRenderer.renderError(content, err.message);
                        }
                    } else {
                        // Why: Triggers immediate UI update with the corresponding translation without page refresh.
                        insightsRenderer.renderReport(this.lastReport);
                    }
                }
            }
        });
    },

    /**
     * @description Sets default date range (last 7 days) to date pickers.
     */
    initDatePickers() {
        const startInput = document.getElementById('reportStartDate');
        const endInput = document.getElementById('reportEndDate');
        if (!startInput || !endInput) return;

        const end = new Date();
        const start = new Date();
        start.setDate(end.getDate() - 7);

        const toISO = (d) => d.toISOString().split('T')[0];
        startInput.value = toISO(start);
        endInput.value = toISO(end);
    },

    /**
     * @description Binds event listeners for report interactions.
     */
    bindReportEvents() {
        const generateBtn = document.getElementById('btnGenerateReport');
        if (generateBtn) {
            generateBtn.addEventListener('click', () => this.generateNewReport());
        }

        const reportList = document.getElementById('reportList');
        if (reportList) {
            reportList.addEventListener('click', async (e) => {
                const item = e.target.closest('.c-report-item');
                const deleteBtn = e.target.closest('.c-report-item__delete');

                // Explicit Integer Conversion
                if (deleteBtn) {
                    const id = parseInt(deleteBtn.dataset.id, 10);
                    if (!isNaN(id)) await this.deleteReport(id);
                    return;
                }

                if (item) {
                    const id = parseInt(item.dataset.id, 10);
                    if (!isNaN(id)) await this.loadReportDetail(id);
                }
            });
        }
    },

    /**
     * @description Generates a new report based on selected date range.
     */
    async generateNewReport() {
        const start = document.getElementById('reportStartDate')?.value;
        const end = document.getElementById('reportEndDate')?.value;
        const btn = document.getElementById('btnGenerateReport');

        if (!start || !end) return;

        try {
            if (btn) btn.disabled = true;
            const result = await api.generateReport(start, end);

            // Refresh list and select the new one
            await this.refreshReport(result.report_id);
        } catch (e) {
            console.error("[Insights] Generate report failed:", e);
            alert(`Report generation failed: ${e.message}`);
        } finally {
            if (btn) btn.disabled = false;
        }
    },

    /**
     * @description Deletes a specific report.
     */
    async deleteReport(id) {
        const lang = state.currentLang || 'ko';
        const i18n = I18N_DATA[lang];
        if (!confirm(i18n.deleteReportConfirm || 'Delete this report?')) return;

        try {
            await api.deleteReport(id);
            await this.refreshReport();
        } catch (e) {
            console.error("[Insights] Delete report failed:", e);
            alert(`Delete failed: ${e.message}`);
        }
    },

    /**
     * @description Fetches and renders the report list.
     */
    async refreshReport(activeId = null) {
        try {
            const reports = await api.fetchReports();
            insightsRenderer.renderReportList(reports, activeId);

            if (activeId) {
                await this.loadReportDetail(activeId);
            } else if (reports.length > 0) {
                await this.loadReportDetail(reports[0].id);
            } else {
                insightsRenderer.renderReport(null);
            }
        } catch (e) {
            console.error("[Insights] Refresh reports failed:", e);
        }
    },

    /**
     * @description Fetches details for a specific report and renders it.
     */
    async loadReportDetail(id) {
        try {
            // Update active state in UI
            document.querySelectorAll('.c-report-item').forEach(item => {
                item.classList.toggle('c-report-item--active', String(item.dataset.id) === String(id));
            });

            const report = await api.fetchReportDetail(id);
            this.lastReport = report;
            insightsRenderer.renderReport(report);
        } catch (e) {
            console.error("[Insights] Load report detail failed:", e);
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
            const [stats, allAch, userAch, tokenUsage] = await Promise.all([
                api.fetchUserStats().catch(e => { console.error("[Insights] Stats failed:", e); return null; }),
                api.fetchAchievements().catch(e => { console.error("[Insights] All achievements failed:", e); return []; }),
                api.fetchUserAchievements().catch(e => { console.error("[Insights] User achievements failed:", e); return []; }),
                api.fetchTokenUsage().catch(e => { console.error("[Insights] Token usage failed:", e); return null; })
            ]);

            this.renderAll(stats, allAch, userAch, tokenUsage);
        } catch (e) {
            console.error("[Insights] Unexpected error during refreshData", e);
        } finally {
            if (loading) {
                loading.classList.remove('active');
                const p = loading.querySelector('p');
                // Why: Restore the default loading message after insights-specific loading is complete.
                if (p) p.textContent = i18n.loading || "Gemini is scanning for new tasks...";
            }
        }
    },

    /**
     * Orchestrates the rendering of all insight components.
     * @param {Object} stats - User statistics data.
     * @param {Array} allAch - All possible achievements.
     * @param {Array} userAch - Achievements earned by the user.
     * @param {Object} tokenUsage - AI token usage data.
     */
    renderAll(stats, allAch, userAch, tokenUsage) {
        if (!stats) return;
        this.lastStats = stats;

        if (tokenUsage && this.tokenCard) {
            this.tokenCard.render(tokenUsage);
        }

        insightsRenderer.renderDailyGlance(stats);
        insightsRenderer.renderActivityHeatmap(stats);
        insightsRenderer.renderSourceDistribution(stats);
        insightsRenderer.renderWaitingMetrics(stats);
        insightsRenderer.renderHourlyActivity(stats);
        insightsRenderer.renderAchievements(allAch, userAch, stats);
        insightsRenderer.renderAnkiChart(stats, this.currentChartDays);
    }
};
