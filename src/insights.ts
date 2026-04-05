/**
 * src/insights.ts
 * Controller for Insights & Analytics module. 
 * Implements strict Tab Isolation & On-Demand Fetching.
 */

import { api } from './api';
import { state } from './state';
import { I18N_DATA } from './locales';
import { insightsRenderer } from './insightsRenderer';
import { events, EVENTS } from './events';
import { UserStats, TokenUsage, IReportData } from './types';

export const insights = {
    lastStats: null as UserStats | null,
    lastReport: null as IReportData | null,
    currentChartDays: 30,

    /**
     * Pure function to extract Daily Performance metrics with Guard Clauses.
     */
    getDailyGlanceMetrics(stats: UserStats | null) {
        if (!stats) return { completed: 0, pending: 0 };
        return {
            completed: stats.total_completed || 0,
            pending: stats.pending_me || 0
        };
    },

    /**
     * Initializes the insights module and sets up tab isolation.
     */
    init() {
        console.log("[Insights] Module Initialized with Tab Isolation");

        // UI Element References
        const statsTab = document.querySelector('.insights-tab-btn[data-tab="insightsStatsTab"]') as HTMLElement | null;
        const reportsTab = document.querySelector('.insights-tab-btn[data-tab="insightsReportsTab"]') as HTMLElement | null;
        const statsPanel = document.getElementById('insightsStatsTab') as HTMLElement | null;
        const reportsPanel = document.getElementById('insightsReportsTab') as HTMLElement | null;
        // insightTabBtns removed: was unused.

        if (statsTab && reportsTab && statsPanel && reportsPanel) {
            // Stats Tab Click
            statsTab.addEventListener('click', async () => {
                this.setActiveTab(statsTab, statsPanel, [reportsTab], [reportsPanel]);
                await this.refreshData(); // Fetch stats only
            });

            // Reports Tab Click
            reportsTab.addEventListener('click', async () => {
                this.setActiveTab(reportsTab, reportsPanel, [statsTab], [statsPanel]);
                
                // Show loading state for reports
                const reportContent = document.getElementById('reportSummaryContent');
                if (reportContent) insightsRenderer.renderLoading(reportContent);
                
                await this.refreshReport(); // Fetch reports on-demand
            });
        }

        // Chart Filter Binding
        document.querySelectorAll('.chart-filters .filter-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;
                document.querySelectorAll('.chart-filters .filter-btn').forEach(b => b.classList.remove('active'));
                target.classList.add('active');
                this.currentChartDays = parseInt(target.dataset.days || '30', 10);

                if (this.lastStats) {
                    insightsRenderer.renderAnkiChart(this.lastStats, this.currentChartDays);
                }
            });
        });

        this.bindReportEvents();
        this.initDatePickers();

        // Global Resize Handling
        window.addEventListener('resize', () => {
            insightsRenderer.resizeAll();
        });

        // Theme Change Handling
        events.on(EVENTS.THEME_CHANGED, () => {
            if (this.isTabActive('insightsStatsTab') && this.lastStats) {
                insightsRenderer.renderAnkiChart(this.lastStats, this.currentChartDays);
            }
            if (this.isTabActive('insightsReportsTab') && this.lastReport) {
                insightsRenderer.renderReportDetail(this.lastReport); // Re-render charts
            }
        });

        // Language Change Handling (JIT Translation)
        events.on(EVENTS.LANGUAGE_CHANGED, async (lang: string) => {
            if (this.isTabActive('insightsReportsTab') && this.lastReport) {
                const reportContent = document.getElementById('reportSummaryContent');
                if (reportContent) insightsRenderer.renderLoading(reportContent);
                
                // If translation doesn't exist, fetch it
                if (!this.lastReport.translations?.[lang]) {
                    try {
                        const result = await api.translateReport(this.lastReport.id, lang);
                        if (!this.lastReport.translations) this.lastReport.translations = {};
                        this.lastReport.translations[lang] = result.summary;
                    } catch (e) {
                        console.error("[Insights] Translation failed:", e);
                    }
                }
                insightsRenderer.renderReportDetail(this.lastReport);
            }
        });
    },

    /**
     * Internal helper to manage tab classes.
     */
    setActiveTab(activeBtn: HTMLElement, activePanel: HTMLElement, inactiveBtns: (HTMLElement|null)[], inactivePanels: (HTMLElement|null)[]) {
        inactiveBtns.forEach(btn => btn?.classList.remove('active'));
        inactivePanels.forEach(p => p?.classList.remove('c-tabs__panel--active'));
        
        activeBtn.classList.add('active');
        activePanel.classList.add('c-tabs__panel--active');
    },

    /**
     * Checks if a specific tab panel is currently visible.
     */
    isTabActive(id: string): boolean {
        const panel = document.getElementById(id);
        return !!panel?.classList.contains('c-tabs__panel--active');
    },

    /**
     * Main entry point when views switch to Insights. 
     * Handles conditional data fetching based on the active sub-tab.
     */
    async onShow() {
        console.log("[Insights] View Shown - Routing to active tab");
        if (this.isTabActive('insightsReportsTab')) {
            await this.refreshReport();
        } else {
            // Default to Stats or if stats is active
            await this.refreshData();
        }
    },

    async initDatePickers() {
        const startInput = document.getElementById('reportStartDate') as HTMLInputElement | null;
        const endInput = document.getElementById('reportEndDate') as HTMLInputElement | null;
        if (!startInput || !endInput) return;

        const end = new Date();
        const start = new Date();
        start.setDate(end.getDate() - 7);

        const toISO = (d: Date) => d.toISOString().split('T')[0];
        startInput.value = toISO(start);
        endInput.value = toISO(end);
    },

    bindReportEvents() {
        const generateBtn = document.getElementById('btnGenerateReport');
        if (generateBtn) {
            generateBtn.addEventListener('click', () => this.generateNewReport());
        }

        const reportList = document.getElementById('reportList');
        if (reportList) {
            reportList.addEventListener('click', async (e) => {
                const target = e.target as HTMLElement;
                const item = target.closest('.c-insights-report-item') as HTMLElement | null;
                const deleteBtn = target.closest('.c-insights-report-item__delete') as HTMLElement | null;

                if (deleteBtn) {
                    const idAttr = deleteBtn.getAttribute('data-id');
                    const id = parseInt(idAttr || '', 10);
                    if (!isNaN(id)) await this.deleteReport(id);
                    return;
                }

                if (item) {
                    const idAttr = item.getAttribute('data-id');
                    const id = parseInt(idAttr || '', 10);
                    if (!isNaN(id)) await this.loadReportDetail(id);
                }
            });
        }
    },

    async generateNewReport() {
        const start = (document.getElementById('reportStartDate') as HTMLInputElement)?.value;
        const end = (document.getElementById('reportEndDate') as HTMLInputElement)?.value;
        const btn = document.getElementById('btnGenerateReport') as HTMLButtonElement;

        if (!start || !end) return;

        try {
            if (btn) btn.disabled = true;
            const result = await api.generateReport(start, end);
            await this.refreshReport(result.report_id);
        } catch (e: any) {
            console.error("[Insights] Generate report failed:", e);
            alert(`Generation failed: ${e.message}`);
        } finally {
            if (btn) btn.disabled = false;
        }
    },

    async deleteReport(id: number) {
        const lang = state.currentLang || 'ko';
        const i18n = (I18N_DATA as any)[lang];
        if (!confirm(i18n.deleteReportConfirm || 'Delete this report?')) return;

        try {
            await api.deleteReport(id);
            await this.refreshReport();
        } catch (e: any) {
            console.error("[Insights] Delete failed:", e);
        }
    },

    async refreshReport(_activeId: number | null = null) {
        // Why: Delegates to insightsRenderer for fetching history and rendering.
        // It now uses the state-first, API-last model.
        try {
            await insightsRenderer.initReportList();
        } catch (e) {
            console.error("[Insights] Refresh reports failed:", e);
        }
    },

    async loadReportDetail(id: number) {
        try {
            // UI Visual Feedback for Sidebar
            document.querySelectorAll('.c-insights-report-item').forEach(item => {
                const el = item as HTMLElement;
                const itemId = el.getAttribute('data-id');
                el.classList.toggle('c-insights-report-item--active', String(itemId) === String(id));
            });

            const report = await api.fetchReportDetail(id);
            this.lastReport = report;
            insightsRenderer.renderReportDetail(report);
        } catch (e) {
            console.error("[Insights] Load report detail failed:", e);
        }
    },

    async refreshData() {
        const loading = document.getElementById('loading');
        if (loading) loading.classList.add('active');

        try {
            const [stats, allAch, userAch, tokenUsage] = await Promise.all([
                api.fetchUserStats().catch(() => null),
                api.fetchAchievements().catch(() => []),
                api.fetchUserAchievements().catch(() => []),
                api.fetchTokenUsage().catch(() => null)
            ]);

            state.userStats = stats;
            this.renderAll(stats, allAch, userAch, tokenUsage);
        } finally {
            if (loading) loading.classList.remove('active');
        }
    },

    renderAll(stats: UserStats | null, allAch: any[], userAch: any[], tokenUsage: TokenUsage | null) {
        if (tokenUsage) insightsRenderer.renderTokenUsage(tokenUsage);
        this.lastStats = stats;

        // Restore Daily Performance Widget with BEM Rendering (Handles null internally)
        insightsRenderer.renderDailyGlance(stats);

        if (stats) {
            insightsRenderer.renderActivityHeatmap(stats); // Bottom heatmap
            insightsRenderer.renderChannelDistribution(stats);
            insightsRenderer.renderHourlyActivity(stats); // Center heatmap (peak integration)
            insightsRenderer.renderStaleTasks(stats);
            insightsRenderer.renderAnkiChart(stats, this.currentChartDays);
            if (allAch && userAch) {
                insightsRenderer.renderAchievements(allAch, userAch, stats);
            }
        }
    }
};
