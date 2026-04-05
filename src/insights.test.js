import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insights } from './insights';
import { api } from './api';
import { insightsRenderer } from './insightsRenderer.ts';

// Mock Modules
vi.mock('./api', () => ({
    api: {
        fetchReports: vi.fn(),
        fetchReportDetail: vi.fn(),
        generateReport: vi.fn(),
        deleteReport: vi.fn(),
        translateReport: vi.fn()
    }
}));

vi.mock('./insightsRenderer.ts', () => ({
    insightsRenderer: {
        renderReportList: vi.fn(),
        renderReportDetail: vi.fn(),
        renderDailyGlance: vi.fn(),
        renderActivityHeatmap: vi.fn(),
        renderSourceDistribution: vi.fn(),
        renderWaitingMetrics: vi.fn(),
        renderHourlyActivity: vi.fn(),
        renderAchievements: vi.fn(),
        renderAnkiChart: vi.fn(),
        renderLoading: vi.fn(),
        renderError: vi.fn(),
        initReportList: vi.fn()
    }
}));

// Mock Globals
vi.stubGlobal('alert', vi.fn());
vi.stubGlobal('confirm', vi.fn(() => true));

describe('insights.js - Controller', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <input id="reportStartDate">
            <input id="reportEndDate">
            <div id="reportList"></div>
            <button id="btnGenerateReport"></button>
            <div id="reportTruncationWarning"></div>
            <div id="dailyGlance"></div>
        `;
        vi.clearAllMocks();
    });

    it('should initialize date pickers correctly', () => {
        insights.initDatePickers();
        const start = document.getElementById('reportStartDate').value;
        const end = document.getElementById('reportEndDate').value;
        
        expect(start).not.toBe('');
        expect(end).not.toBe('');
        expect(new Date(end).getTime()).toBeGreaterThanOrEqual(new Date(start).getTime());
    });

    it('should refresh reports and render the list', async () => {
        const mockReports = [{ id: 1, start_date: '2024-03-01', end_date: '2024-03-07' }];
        api.fetchReports.mockResolvedValue(mockReports);
        api.fetchReportDetail.mockResolvedValue(mockReports[0]);

        await insights.refreshReport();
        expect(insightsRenderer.initReportList).toHaveBeenCalled();
    });

    it('should load report details when selected', async () => {
        const mockReport = { id: 1, report_summary: 'Test Content' };
        api.fetchReportDetail.mockResolvedValue(mockReport);

        await insights.loadReportDetail(1);

        expect(api.fetchReportDetail).toHaveBeenCalledWith(1);
        expect(insightsRenderer.renderReportDetail).toHaveBeenCalledWith(mockReport);
    });

    it('should handle report creation with validation', async () => {
        const startInput = document.getElementById('reportStartDate');
        const endInput = document.getElementById('reportEndDate');
        
        // Valid dates
        startInput.value = '2024-03-01';
        endInput.value = '2024-03-07';
        api.generateReport.mockResolvedValue({ report_id: 99 });
        api.fetchReports.mockResolvedValue([]);
        api.fetchReportDetail.mockResolvedValue({ id: 99 });

        await insights.generateNewReport();
        expect(api.generateReport).toHaveBeenCalledWith('2024-03-01', '2024-03-07');
    });

    it('should show alert when report generation fails', async () => {
        const startInput = document.getElementById('reportStartDate');
        const endInput = document.getElementById('reportEndDate');
        startInput.value = '2024-03-01';
        endInput.value = '2024-03-07';
        
        api.generateReport.mockRejectedValue(new Error('AI limit reached'));

        await insights.generateNewReport();
        expect(alert).toHaveBeenCalledWith(expect.stringContaining('AI limit reached'));
    });

    it('should confirm before deleting a report', async () => {
        api.deleteReport.mockResolvedValue({ success: true });
        api.fetchReports.mockResolvedValue([]);

        await insights.deleteReport(1);

        expect(confirm).toHaveBeenCalled();
        expect(api.deleteReport).toHaveBeenCalledWith(1);
    });

    it('should handle JIT translation on language change', async () => {
        // Setup state
        document.body.innerHTML += `
            <div id="insightsSection"></div>
            <div id="reportSummaryContent"></div>
            <div id="insightsStatsTab" class="c-tabs__panel"></div>
            <div id="insightsReportsTab" class="c-tabs__panel c-tabs__panel--active"></div>
            <button class="insights-tab-btn" data-tab="insightsStatsTab"></button>
            <button class="insights-tab-btn" data-tab="insightsReportsTab"></button>
        `;
        insights.init(); // Register listeners
        
        const mockReport = { id: 1, report_summary: 'English', translations: {} };
        insights.lastReport = mockReport;
        
        // Mock API
        api.translateReport.mockResolvedValue({ summary: 'Translated Summary' });

        // Trigger event
        const { events, EVENTS } = await import('./events');
        events.emit(EVENTS.LANGUAGE_CHANGED, 'ko');

        // Verify loading was shown
        expect(insightsRenderer.renderLoading).toHaveBeenCalled();
        
        // Wait for async call
        await vi.waitFor(() => expect(api.translateReport).toHaveBeenCalledWith(1, 'ko'));
        
        // Verify report was re-rendered with new translation
        expect(insightsRenderer.renderReportDetail).toHaveBeenCalled();
        expect(mockReport.translations.ko).toBe('Translated Summary');
    });
});
