/**
 * src/insights.ts
 * Controller for Insights & Analytics module. 
 * Implements strict Tab Isolation & On-Demand Fetching.
 */

import { api } from './api';
import { state, upsertReport, removeReportFromState, updateReportHistory } from './state';
import { I18N_DATA } from './locales';
import { insightsRenderer } from './insightsRenderer';
import { normalizeReportData } from './logic';
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
        (window as any).insights = this; // Expose for renderer callbacks

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

        // Handle empty state generate button through custom event
        window.addEventListener('generate-report-clicked', () => {
            this.handleGenerateClick();
        });

        // Theme Change Handling
        events.on(EVENTS.THEME_CHANGED, () => {
            const lang = state.currentLang || 'en';
            const i18n = I18N_DATA[lang];
            if (this.isTabActive('insightsStatsTab') && this.lastStats) {
                insightsRenderer.renderAnkiChart(this.lastStats, this.currentChartDays);
            }
            if (this.isTabActive('insightsReportsTab') && this.lastReport) {
                insightsRenderer.renderReport(this.lastReport, lang, i18n); // Re-render charts
            }
        });

        // Language Change Handling (JIT Translation)
        events.on(EVENTS.LANGUAGE_CHANGED, async (lang: string) => {
            const i18n = I18N_DATA[lang || 'en'];
            if (this.isTabActive('insightsReportsTab') && this.lastReport) {
                const reportContent = document.getElementById('reportSummaryContent');
                if (reportContent) insightsRenderer.renderLoading(reportContent, i18n, 'translation');
                
                // If translation doesn't exist, fetch it
                if (!this.lastReport.translations?.[lang]) {
                    try {
                        const result = await api.translateReport(this.lastReport.id, lang);
                        if (!this.lastReport.translations) this.lastReport.translations = {};
                        
                        // Defensively extract translated text from various possible response fields
                        const translatedText = result.summary || result.report_summary || result.translation || result.translated_text || (typeof result === 'string' ? result : '');
                        
                        this.lastReport.translations[lang] = translatedText;
                        
                        // Persist to global state (AppState.reports) for O(1) tab-switching retrieval
                        upsertReport(this.lastReport);
                    } catch (e) {
                        console.error("[Insights] Translation failed:", e);
                    }
                }
                insightsRenderer.renderReport(this.lastReport, lang, i18n);
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

        // Filter UI bindings
        document.getElementById('reportChannelFilter')?.addEventListener('change', () => this.refreshFilterIndicators());
        document.getElementById('reportStatusFilter')?.addEventListener('change', () => this.refreshFilterIndicators());

        const reportList = document.getElementById('reportList');
        if (reportList) {
            reportList.addEventListener('click', async (e) => {
                const target = e.target as HTMLElement;
                const deleteBtn = target.closest('.c-insights-report-item__delete') as HTMLElement | null;

                if (deleteBtn) {
                    const idAttr = deleteBtn.getAttribute('data-id');
                    const id = parseInt(idAttr || '', 10);
                    if (!isNaN(id)) await this.deleteReport(id);
                }
                // Item click is now handled via callback in initReportList
            });
        }
    },
    
    async handleGenerateClick() {
        const today = new Date().toISOString().split('T')[0];
        try {
            const result = await api.generateReport(today, today);
            await this.refreshReport(result.id || (result as any).report_id);
        } catch (e: any) {
            console.error("[Insights] Automatic generation failed:", e);
            alert(`Generation failed: ${e.message}`);
        }
    },

    async generateNewReport() {
        const start = (document.getElementById('reportStartDate') as HTMLInputElement)?.value;
        const end = (document.getElementById('reportEndDate') as HTMLInputElement)?.value;
        const channelId = (document.getElementById('reportChannelFilter') as HTMLSelectElement)?.value;
        const status = (document.getElementById('reportStatusFilter') as HTMLSelectElement)?.value;

        const btn = document.getElementById('btnGenerateReport') as HTMLButtonElement;
        const reportContent = document.getElementById('reportSummaryContent');
        const i18n = I18N_DATA[state.currentLang || 'en'];

        if (!start || !end) return;

        try {
            if (btn) btn.disabled = true;
            if (reportContent) insightsRenderer.renderLoading(reportContent, i18n, 'report');
            
            const result = await api.generateReport(start, end, channelId, status);
            const report = normalizeReportData(result);
            
            if (report.status === 'processing') {
                this.pollReportStatus(report.id);
            } else {
                await this.refreshReport(report.id);
            }
        } catch (e: any) {
            console.error("[Insights] Generate report failed:", e);
            if (reportContent) insightsRenderer.renderError(reportContent, e.message, i18n);
            alert(`Generation failed: ${e.message}`);
        } finally {
            if (btn) btn.disabled = false;
        }
    },

    refreshFilterIndicators() {
        // Visual feedback when filters are active
        const channel = (document.getElementById('reportChannelFilter') as HTMLSelectElement)?.value;
        const status = (document.getElementById('reportStatusFilter') as HTMLSelectElement)?.value;
        const generateBtn = document.getElementById('btnGenerateReport');
        
        if (generateBtn) {
            if (channel || status) {
                generateBtn.classList.add('c-btn--pulse');
            } else {
                generateBtn.classList.remove('c-btn--pulse');
            }
        }
    },

    /**
     * Polls for report completion when in 'processing' state.
     */
    async pollReportStatus(id: number, attempts = 0) {
        if (attempts > 30) { // 30 attempts * 5s = 150s (Just over GCP timeout)
            this.handlePollTimeout();
            return;
        }

        try {
            const raw = await api.fetchReportDetail(id);
            const report = normalizeReportData(raw);

            if (report.status === 'completed') {
                await this.refreshReport(id);
            } else if (report.status === 'failed') {
                this.handlePollFailure();
            } else {
                // Still processing
                setTimeout(() => this.pollReportStatus(id, attempts + 1), 5000);
            }
        } catch (e) {
            console.error("[Insights] Polling failed:", e);
            setTimeout(() => this.pollReportStatus(id, attempts + 1), 5000);
        }
    },

    handlePollTimeout() {
        const i18n = I18N_DATA[state.currentLang || 'en'];
        const reportContent = document.getElementById('reportSummaryContent');
        if (reportContent) insightsRenderer.renderError(reportContent, i18n.reportTimeout || "Generation taking too long. Please refresh later.", i18n);
    },

    handlePollFailure() {
        const i18n = I18N_DATA[state.currentLang || 'en'];
        const reportContent = document.getElementById('reportSummaryContent');
        if (reportContent) insightsRenderer.renderError(reportContent, i18n.reportFailed || "Generation failed.", i18n);
    },

    async deleteReport(id: number) {
        const lang = state.currentLang || 'en';
        const i18n = (I18N_DATA as any)[lang];
        if (!confirm(i18n.deleteReportConfirm || 'Delete this report?')) return;

        try {
            // Find report in history to get dates for cache invalidation
            const reportMeta = state.reportHistory.find(r => r.id === id);
            if (reportMeta) removeReportFromState(reportMeta.start_date, reportMeta.end_date);

            await api.deleteReport(id);
            await this.refreshReport();

            // UX Fallback: If deleted report was active, load newest one
            if (this.lastReport?.id === id) {
                this.handleDeletionFallback();
            }
        } catch (e: any) {
            console.error("[Insights] Delete failed:", e);
        }
    },

    /**
     * Handles UI fallback after deleting the active report.
     */
    handleDeletionFallback() {
        this.lastReport = null;
        const i18n = I18N_DATA[state.currentLang || 'en'];
        if (state.reportHistory.length > 0) {
            this.loadExistingReport(state.reportHistory[0]);
            return;
        }
        insightsRenderer.renderEmptyState(i18n);
    },

    async refreshReport(_activeId: number | null = null) {
        const i18n = I18N_DATA[state.currentLang || 'en'];
        const reportContent = document.getElementById('reportSummaryContent');
        try {
            const history = await api.fetchReportHistory();
            updateReportHistory(history);
            insightsRenderer.renderReportList(state.reportHistory, i18n, _activeId);

            if (state.reportHistory.length === 0) {
                // No reports: show empty state immediately (no spinner needed)
                insightsRenderer.renderEmptyState(i18n);
                return;
            }

            // Auto-load the most recent report only when no specific report is active
            if (_activeId === null && !this.lastReport) {
                await this.loadExistingReport(state.reportHistory[0]);
            } else if (_activeId !== null) {
                const target = state.reportHistory.find(r => r.id === _activeId);
                if (target) await this.loadExistingReport(target);
            }
        } catch (e) {
            console.error("[Insights] Refresh reports failed:", e);
            if (reportContent) insightsRenderer.renderError(reportContent, (e as any).message, i18n);
        }
    },

    /**
     * Dual-Layer Cache Loading strategy.
     * Checks local state before fetching from API.
     */
    async loadExistingReport(reportMetadata: IReportData) {
        const lang = state.currentLang || 'en';
        const i18n = I18N_DATA[lang];
        const reportContent = document.getElementById('reportSummaryContent');
        const key = `${reportMetadata.start_date}_${reportMetadata.end_date}`;
        
        try {
            // Level 1: Memory Cache hit?
            if (state.reports[key] && state.reports[key].report_summary) {
                this.lastReport = state.reports[key];
                insightsRenderer.renderReport(this.lastReport, lang, i18n);
                return;
            }

            // Level 2: API Fetch with spinner
            if (reportContent) insightsRenderer.renderLoading(reportContent, i18n, 'load');
            
            const rawReport = await api.fetchReportDetail(reportMetadata.id);
            const report = normalizeReportData(rawReport);
            
            this.lastReport = report;
            upsertReport(report);

            if (report.status === 'processing') {
                this.pollReportStatus(report.id);
            } else if (report.status === 'failed') {
                if (reportContent) insightsRenderer.renderError(reportContent, i18n.reportFailed || "Generation failed.", i18n);
            } else {
                insightsRenderer.renderReport(report, lang, i18n);
            }
        } catch (e: any) {
            console.error("[Insights] Load existing report failed:", e);
            // Guarantee spinner is cleared even on silent failure
            if (reportContent) insightsRenderer.renderError(reportContent, e.message, i18n);
        }
    },

    async refreshData() {
        const loading = document.getElementById('loading');
        if (loading) loading.classList.add('active');

        try {
            const [stats, _, __, tokenUsage] = await Promise.all([
                api.fetchUserStats().catch(() => null),
                api.fetchAchievements().catch(() => []),
                api.fetchUserAchievements().catch(() => []),
                api.fetchTokenUsage().catch(() => null)
            ]);

            state.userStats = stats;
            this.renderAll(stats, tokenUsage);
        } finally {
            if (loading) loading.classList.remove('active');
        }
    },

    renderAll(stats: UserStats | null, tokenUsage: TokenUsage | null) {
        const lang = state.currentLang || 'en';
        const i18n = I18N_DATA[lang];

        if (tokenUsage) {
            insightsRenderer.renderTokenUsage(tokenUsage, i18n);
            insightsRenderer.renderFilteredNoise(tokenUsage, i18n);
        }
        this.lastStats = stats;

        // Restore Daily Performance Widget with BEM Rendering (Handles null internally)
        insightsRenderer.renderDailyGlance(stats, i18n);

        if (stats) {
            insightsRenderer.renderActivityHeatmap(stats, i18n); // Bottom heatmap
            insightsRenderer.renderChannelDistribution(stats, i18n);
            insightsRenderer.renderHourlyActivity(stats, i18n); // Center heatmap (peak integration)
            insightsRenderer.renderStaleTasks(stats, i18n);
            insightsRenderer.renderAnkiChart(stats, this.currentChartDays);
        }
    }
};
