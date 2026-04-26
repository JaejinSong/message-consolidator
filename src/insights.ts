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
import { getErrorMessage } from './utils';

export const insights = {
    lastStats: null as UserStats | null,
    lastReport: null as IReportData | null,
    currentChartDays: 30,

    /**
     * Initializes the insights module and sets up tab isolation.
     */
    init() {
        console.log("[Insights] Module Initialized with Tab Isolation");
        window.insights = this; // Expose for renderer callbacks

        // Sub-Tab Navigation inside Insights Section (Event Delegation)
        const insightsTabsContainer = document.querySelector('#insightsSection .c-tabs');
        if (insightsTabsContainer) {
            insightsTabsContainer.addEventListener('click', async (e) => {
                const target = (e.target as HTMLElement).closest('.insights-tab-btn') as HTMLElement;
                if (!target) return;
                
                const tabId = target.getAttribute('data-tab');
                if (tabId) {
                    const activePanel = document.getElementById(tabId);
                    const allTabs = Array.from(document.querySelectorAll('.insights-tab-btn')) as HTMLElement[];
                    const inactiveBtns = allTabs.filter(b => b !== target);
                    
                    const inactivePanels = allTabs
                        .filter(b => b !== target)
                        .map(b => document.getElementById(b.getAttribute('data-tab') || ''))
                        .filter((p): p is HTMLElement => p !== null);

                    if (activePanel) {
                        this.setActiveTab(target, activePanel, inactiveBtns, inactivePanels);
                        if (tabId === 'insightsStatsTab') await this.refreshData();
                        if (tabId === 'insightsReportsTab') await this.refreshReport();
                    }
                }
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
                    insightsRenderer.renderCompletionTrend(this.lastStats, this.currentChartDays);
                }
            });
        });

        this.bindReportEvents();
        this.initDatePickers();

        // Theme Change Handling
        events.on(EVENTS.THEME_CHANGED, () => {
            const lang = state.currentLang || 'en';
            const i18n = I18N_DATA[lang];
            if (this.isTabActive('insightsStatsTab') && this.lastStats) {
                insightsRenderer.renderCompletionTrend(this.lastStats, this.currentChartDays);
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
                await this._renderWithTranslation(this.lastReport, lang, i18n, reportContent);
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

        const exportBtn = document.getElementById('btnExportPDF');
        if (exportBtn) {
            exportBtn.addEventListener('click', () => this.exportToPDF());
        }

        const notionBtn = document.getElementById('btnExportNotion');
        if (notionBtn) {
            notionBtn.addEventListener('click', () => this.exportToNotion());
        }

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
                await this.refreshReport();
                this.pollReportStatus(report.id);
            } else {
                const lang = state.currentLang || 'en';
                this.lastReport = report;
                upsertReport(report);
                await this._renderWithTranslation(report, lang, i18n, reportContent);
                await this.refreshReport(report.id);
            }
        } catch (e: unknown) {
            console.error("[Insights] Generate report failed:", e);
            const msg = getErrorMessage(e);
            if (reportContent) insightsRenderer.renderError(reportContent, msg, i18n);
            alert(`Generation failed: ${msg}`);
        } finally {
            if (btn) btn.disabled = false;
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
        const i18n = I18N_DATA[lang];
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
        } catch (e: unknown) {
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
            const rawHistory = await api.fetchReportHistory();
            const history = Array.isArray(rawHistory) ? rawHistory : [];
            updateReportHistory(history);

            // Access reportHistory from state safely
            const reportHistory = state.reportHistory || [];
            insightsRenderer.renderReportList(reportHistory, i18n, _activeId);

            if (reportHistory.length === 0) {
                // No reports: show empty state immediately (no spinner needed)
                insightsRenderer.renderEmptyState(i18n);
                return;
            }

            // Auto-load the most recent report only when no specific report is active
            if (_activeId === null && !this.lastReport) {
                await this.loadExistingReport(reportHistory[0]);
            } else if (_activeId !== null) {
                const target = reportHistory.find(r => r.id === _activeId);
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
                await this._renderWithTranslation(this.lastReport, lang, i18n, reportContent);
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
                await this._renderWithTranslation(report, lang, i18n, reportContent);
            }
        } catch (e: unknown) {
            console.error("[Insights] Load existing report failed:", e);
            // Guarantee spinner is cleared even on silent failure
            if (reportContent) insightsRenderer.renderError(reportContent, getErrorMessage(e), i18n);
        }
    },

    async _renderWithTranslation(report: IReportData, lang: string, i18n: any, reportContent: HTMLElement | null) {
        if (lang !== 'en' && !report.translations?.[lang]) {
            if (reportContent) insightsRenderer.renderLoading(reportContent, i18n, 'translation');
            try {
                const result = await api.translateReport(report.id, lang);
                if (!report.translations) report.translations = {};
                const translatedText = result.report_summary || result.summary || result.translation || result.translated_text || (typeof result === 'string' ? result : '');
                if (translatedText) {
                    report.translations[lang] = translatedText;
                    upsertReport(report);
                }
            } catch (e) {
                console.error("[Insights] Translation failed:", e);
            }
        }
        insightsRenderer.renderReport(report, lang, i18n);
        document.getElementById('btnExportPDF')?.classList.remove('u-hidden');
        document.getElementById('btnExportNotion')?.classList.remove('u-hidden');
    },

    async exportToNotion() {
        const report = this.lastReport;
        if (!report?.id) return;

        const btn = document.getElementById('btnExportNotion') as HTMLButtonElement;
        const original = btn.textContent ?? 'Notion';
        btn.textContent = '저장 중...';
        btn.disabled = true;

        try {
            const data = await api.exportReportToNotion(report.id);
            if (!data.url) throw new Error(data.error ?? 'Export failed');
            window.open(data.url, '_blank');
        } catch (e) {
            alert(`Notion 내보내기 실패: ${e instanceof Error ? e.message : String(e)}`);
        } finally {
            btn.textContent = original;
            btn.disabled = false;
        }
    },

    exportToPDF() {
        const report = this.lastReport;
        const prevTitle = document.title;
        if (report?.start_date && report?.end_date) {
            document.title = `Report_${report.start_date}_${report.end_date}`;
        }
        window.print();
        document.title = prevTitle;
    },

    async refreshData() {
        const loading = document.getElementById('loading');
        if (loading) loading.classList.add('active');

        try {
            const [stats, tokenUsage] = await Promise.all([
                api.fetchUserStats().catch(() => null),
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
            insightsRenderer.renderCompletionTrend(stats, this.currentChartDays);
        }
    }
};
