import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insights } from './insights';
import { api } from './api';
import { insightsRenderer } from './insightsRenderer';
import { upsertReport, state } from './state';
import { I18N_DATA } from './locales';

// Mock Modules
vi.mock('./api', () => ({
    api: {
        fetchReports: vi.fn(),
        fetchReportHistory: vi.fn(),
        fetchReportDetail: vi.fn(),
        generateReport: vi.fn(),
        deleteReport: vi.fn(),
        translateReport: vi.fn()
    }
}));

vi.mock('./insightsRenderer', () => ({
    insightsRenderer: {
        renderReportList: vi.fn(),
        renderReport: vi.fn(),
        renderDailyGlance: vi.fn(),
        renderActivityHeatmap: vi.fn(),
        renderSourceDistribution: vi.fn(),
        renderWaitingMetrics: vi.fn(),
        renderHourlyActivity: vi.fn(),

        renderAnkiChart: vi.fn(),
        renderLoading: vi.fn(),
        renderError: vi.fn(),
        renderTokenUsage: vi.fn(),
        renderFilteredNoise: vi.fn(),
        renderChannelDistribution: vi.fn(),
        renderStaleTasks: vi.fn(),
        renderEmptyState: vi.fn(),
        resizeAll: vi.fn()
    }
}));

vi.mock('./state', () => ({
    state: {
        currentLang: 'ko',
        reportHistory: [],
        reports: {}
    },
    upsertReport: vi.fn(),
    updateReportHistory: vi.fn(),
    removeReportFromState: vi.fn()
}));

vi.mock('./locales', () => ({
    I18N_DATA: {
        ko: { generatingReport: '생성 중', loadingData: '불러오는 중', generatingTranslation: '번역 중' },
        en: { generatingReport: 'Generating', loadingData: 'Loading', generatingTranslation: 'Translating' }
    }
}));

// Mock Globals
vi.stubGlobal('alert', vi.fn());
vi.stubGlobal('confirm', vi.fn(() => true));

describe('insights.ts - Controller (Passive View Refactor)', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="insightsStatsTab" class="c-tabs__panel"></div>
            <div id="insightsReportsTab" class="c-tabs__panel c-tabs__panel--active"></div>
            <input id="reportStartDate">
            <input id="reportEndDate">
            <div id="reportList"></div>
            <button id="btnGenerateReport"></button>
            <div id="reportSummaryContent"></div>
            <div id="dailyGlanceValue"></div>
            <div id="dailyGlanceDetail"></div>
        `;
        vi.clearAllMocks();
        state.currentLang = 'ko';
    });

    it('should refresh reports and render the list with i18n injection', async () => {
        const mockHistory = [{ id: 1, start_date: '2024-03-01', end_date: '2024-03-07' }];
        api.fetchReportHistory.mockResolvedValue(mockHistory);
        state.reportHistory = mockHistory;

        await insights.refreshReport();

        expect(api.fetchReportHistory).toHaveBeenCalled();
        expect(insightsRenderer.renderReportList).toHaveBeenCalledWith(
            mockHistory,
            expect.objectContaining({ generatingReport: '생성 중' }),
            null
        );
    });

    it('should load report details with DI (report, lang, i18n)', async () => {
        const reportMeta = { id: 1, start_date: '2024-03-01', end_date: '2024-03-07' };
        const mockReport = { ...reportMeta, report_summary: 'Test Content' };
        api.fetchReportDetail.mockResolvedValue(mockReport);

        await insights.loadExistingReport(reportMeta);

        expect(api.fetchReportDetail).toHaveBeenCalledWith(1);
        expect(insightsRenderer.renderReport).toHaveBeenCalledWith(
            expect.objectContaining({ report_summary: 'Test Content' }),
            'ko',
            expect.any(Object)
        );
    });

    it('should inject correct i18n message when loading report', async () => {
        const reportMeta = { id: 1, start_date: '2024-03-01', end_date: '2024-03-07' };
        api.fetchReportDetail.mockResolvedValue({ id: 1 });

        await insights.loadExistingReport(reportMeta);

        expect(insightsRenderer.renderLoading).toHaveBeenCalledWith(
            expect.any(HTMLElement),
            expect.objectContaining({ loadingData: '불러오는 중' }),
            'load'
        );
    });

    it('should handle JIT translation with injected i18n on language change', async () => {
        // Manually trigger init to bind events
        insights.init(); 
        
        const mockReport = { id: 1, report_summary: 'English Original', translations: {} };
        insights.lastReport = mockReport;
        
        api.translateReport.mockResolvedValue({ report_summary: '번역된 요약' });

        const { events, EVENTS } = await import('./events');
        events.emit(EVENTS.LANGUAGE_CHANGED, 'ko');

        // Check loading state injection
        expect(insightsRenderer.renderLoading).toHaveBeenCalledWith(
            expect.any(HTMLElement),
            expect.objectContaining({ generatingTranslation: '번역 중' }),
            'translation'
        );

        await vi.waitFor(() => expect(api.translateReport).toHaveBeenCalledWith(1, 'ko'));
        
        expect(insightsRenderer.renderReport).toHaveBeenCalledWith(
            mockReport,
            'ko',
            expect.objectContaining({ generatingTranslation: '번역 중' })
        );
    });

    it('should render all widgets with i18n injection in renderAll', () => {
        const stats = { total_completed: 10, completion_history: [] };
        const i18nKo = I18N_DATA.ko;

        insights.renderAll(stats, { todayTotal: 100 });

        expect(insightsRenderer.renderTokenUsage).toHaveBeenCalledWith(expect.any(Object), i18nKo);
        expect(insightsRenderer.renderDailyGlance).toHaveBeenCalledWith(stats, i18nKo);
        expect(insightsRenderer.renderActivityHeatmap).toHaveBeenCalledWith(stats, i18nKo);
    });
});
