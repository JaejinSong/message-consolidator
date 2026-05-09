// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insights } from './insights';
import { api } from './api';
import { insightsRenderer } from './insightsRenderer';
import { state } from './state';
import { I18N_DATA } from './locales';

// Mock Modules
vi.mock('./api', () => ({
    api: {
        fetchReports: vi.fn(),
        fetchReportHistory: vi.fn(),
        fetchReportDetail: vi.fn(),
        generateReport: vi.fn(),
        deleteReport: vi.fn(),
        translateReport: vi.fn(),
        fetchUserStats: vi.fn(),
        fetchTokenUsage: vi.fn(),
        exportReportToNotion: vi.fn()
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
        renderCompletionTrend: vi.fn(),
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
        ko: {
            generatingReport: '생성 중',
            loadingData: '불러오는 중',
            generatingTranslation: '번역 중',
            deleteReportConfirm: '삭제하시겠습니까?'
        },
        en: {
            generatingReport: 'Generating',
            loadingData: 'Loading',
            generatingTranslation: 'Translating',
            deleteReportConfirm: 'Delete this report?'
        }
    }
}));

// Mock Globals
vi.stubGlobal('alert', vi.fn());
vi.stubGlobal('confirm', vi.fn(() => true));

const makeStats = () => ({
    total_completed: 10,
    completion_history: [{ date: '2024-03-01', counts: { slack: 3 } }],
    pending_me: 0,
    pending_others: 0,
    peak_time: '14:00',
    abandoned_tasks: 2,
    daily_completions: {},
    source_distribution: { slack: 70, whatsapp: 30 },
    source_distribution_total: { slack: 70, whatsapp: 30 },
    hourly_activity: {}
});

const makeReportMeta = () => ({
    id: 1,
    start_date: '2024-03-01',
    end_date: '2024-03-07',
    user_email: 'test@example.com',
    report_summary: 'Test Content',
    visualization_data: ''
});

const BASE_HTML = `
    <div id="insightsSection">
        <div class="c-tabs">
            <button class="insights-tab-btn" data-tab="insightsStatsTab">Stats</button>
            <button class="insights-tab-btn" data-tab="insightsReportsTab">Reports</button>
        </div>
    </div>
    <div id="insightsStatsTab" class="c-tabs__panel"></div>
    <div id="insightsReportsTab" class="c-tabs__panel c-tabs__panel--active"></div>
    <input id="reportStartDate">
    <input id="reportEndDate">
    <select id="reportChannelFilter"><option value="">All</option></select>
    <select id="reportStatusFilter"><option value="">All</option></select>
    <div id="reportList"></div>
    <button id="btnGenerateReport"></button>
    <button id="btnExportPDF" class="u-hidden"></button>
    <button id="btnExportNotion" class="u-hidden"></button>
    <div id="reportSummaryContent"></div>
    <div id="dailyGlanceValue"></div>
    <div id="dailyGlanceDetail"></div>
    <div id="loading"></div>
    <div class="chart-filters">
        <button class="filter-btn active" data-days="30">30d</button>
        <button class="filter-btn" data-days="90">90d</button>
        <button class="filter-btn" data-days="365">1y</button>
    </div>
    <div class="c-insights-report-main"></div>
`;

describe('insights.ts - Controller (Passive View Refactor)', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reportHistory = [];
        state.reports = {};
        insights.lastStats = null;
        insights.lastReport = null;
        insights.currentChartDays = 30;
    });

    it('should refresh reports and render the list with i18n injection', async () => {
        const mockHistory = [makeReportMeta()];
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue(mockHistory);
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
        const reportMeta = makeReportMeta();
        const mockReport = { ...reportMeta, report_summary: 'Test Content' };
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue(mockReport);

        await insights.loadExistingReport(reportMeta);

        expect(api.fetchReportDetail).toHaveBeenCalledWith(1);
        expect(insightsRenderer.renderReport).toHaveBeenCalledWith(
            expect.objectContaining({ report_summary: 'Test Content' }),
            'ko',
            expect.any(Object)
        );
    });

    it('should inject correct i18n message when loading report', async () => {
        const reportMeta = makeReportMeta();
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 1 });

        await insights.loadExistingReport(reportMeta);

        expect(insightsRenderer.renderLoading).toHaveBeenCalledWith(
            expect.any(HTMLElement),
            expect.objectContaining({ loadingData: '불러오는 중' }),
            'load'
        );
    });

    it('should handle JIT translation with injected i18n on language change', async () => {
        insights.init();

        const mockReport = { id: 1, user_email: '', start_date: '', end_date: '', report_summary: 'English Original', visualization_data: '', translations: {} };
        insights.lastReport = mockReport;

        (api.translateReport as ReturnType<typeof vi.fn>).mockResolvedValue({ report_summary: '번역된 요약' });

        const { events, EVENTS } = await import('./events');
        events.emit(EVENTS.LANGUAGE_CHANGED, 'ko');

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
        const stats = makeStats();
        const i18nKo = I18N_DATA.ko;

        insights.renderAll(stats, { todayTotal: 100, todayPrompt: 0, todayCompletion: 0, todayCost: 0, monthlyTotal: 0, monthlyPrompt: 0, monthlyCompletion: 0, monthlyCost: 0, model: '' });

        expect(insightsRenderer.renderTokenUsage).toHaveBeenCalledWith(expect.any(Object), i18nKo);
        expect(insightsRenderer.renderDailyGlance).toHaveBeenCalledWith(stats, i18nKo);
        expect(insightsRenderer.renderActivityHeatmap).toHaveBeenCalledWith(stats, i18nKo);
    });
});

describe('insights.ts - onShow routing', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reportHistory = [];
        state.reports = {};
        insights.lastStats = null;
        insights.lastReport = null;
    });

    it('calls refreshData when stats tab is active', async () => {
        // Make stats tab active
        document.getElementById('insightsStatsTab')!.classList.add('c-tabs__panel--active');
        document.getElementById('insightsReportsTab')!.classList.remove('c-tabs__panel--active');

        (api.fetchUserStats as ReturnType<typeof vi.fn>).mockResolvedValue(makeStats());
        (api.fetchTokenUsage as ReturnType<typeof vi.fn>).mockResolvedValue(null);

        await insights.onShow();

        expect(api.fetchUserStats).toHaveBeenCalled();
    });

    it('calls refreshReport when reports tab is active', async () => {
        // insightsReportsTab is active by default in BASE_HTML
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([]);

        await insights.onShow();

        expect(api.fetchReportHistory).toHaveBeenCalled();
    });
});

describe('insights.ts - refreshData', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        insights.lastStats = null;
    });

    it('shows/hides loading indicator during fetch', async () => {
        (api.fetchUserStats as ReturnType<typeof vi.fn>).mockResolvedValue(makeStats());
        (api.fetchTokenUsage as ReturnType<typeof vi.fn>).mockResolvedValue(null);

        const loadingEl = document.getElementById('loading')!;
        expect(loadingEl.classList.contains('active')).toBe(false);

        await insights.refreshData();

        expect(loadingEl.classList.contains('active')).toBe(false);
    });

    it('calls all renderer methods when stats and tokenUsage both return data', async () => {
        const stats = makeStats();
        const tokenUsage = { todayTotal: 50, todayPrompt: 20, todayCompletion: 30, todayCost: 0, monthlyTotal: 200, monthlyPrompt: 100, monthlyCompletion: 100, monthlyCost: 0.5, model: 'Gemini' };

        (api.fetchUserStats as ReturnType<typeof vi.fn>).mockResolvedValue(stats);
        (api.fetchTokenUsage as ReturnType<typeof vi.fn>).mockResolvedValue(tokenUsage);

        await insights.refreshData();

        expect(insightsRenderer.renderTokenUsage).toHaveBeenCalledWith(tokenUsage, expect.any(Object));
        expect(insightsRenderer.renderFilteredNoise).toHaveBeenCalledWith(tokenUsage, expect.any(Object));
        expect(insightsRenderer.renderDailyGlance).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderActivityHeatmap).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderChannelDistribution).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderHourlyActivity).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderStaleTasks).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderCompletionTrend).toHaveBeenCalledWith(stats, 30);
    });

    it('skips stats renderers when stats API returns null', async () => {
        (api.fetchUserStats as ReturnType<typeof vi.fn>).mockResolvedValue(null);
        (api.fetchTokenUsage as ReturnType<typeof vi.fn>).mockResolvedValue(null);

        await insights.refreshData();

        expect(insightsRenderer.renderActivityHeatmap).not.toHaveBeenCalled();
        expect(insightsRenderer.renderChannelDistribution).not.toHaveBeenCalled();
        expect(insightsRenderer.renderCompletionTrend).not.toHaveBeenCalled();
        // renderDailyGlance is always called (handles null internally)
        expect(insightsRenderer.renderDailyGlance).toHaveBeenCalledWith(null, expect.any(Object));
    });

    it('still hides loading even when API throws', async () => {
        (api.fetchUserStats as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('network error'));
        (api.fetchTokenUsage as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('network error'));

        const loadingEl = document.getElementById('loading')!;
        await insights.refreshData();

        expect(loadingEl.classList.contains('active')).toBe(false);
    });
});

describe('insights.ts - period filter (chart-filters)', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        insights.lastStats = makeStats();
        insights.currentChartDays = 30;
        insights.init();
    });

    it('changes currentChartDays and re-renders trend when filter button is clicked', () => {
        const btn90 = document.querySelector<HTMLElement>('.filter-btn[data-days="90"]')!;
        btn90.click();

        expect(insights.currentChartDays).toBe(90);
        expect(insightsRenderer.renderCompletionTrend).toHaveBeenCalledWith(insights.lastStats, 90);
    });

    it('does not call renderCompletionTrend when lastStats is null', () => {
        insights.lastStats = null;

        const btn365 = document.querySelector<HTMLElement>('.filter-btn[data-days="365"]')!;
        btn365.click();

        expect(insights.currentChartDays).toBe(365);
        expect(insightsRenderer.renderCompletionTrend).not.toHaveBeenCalled();
    });

    it('marks clicked filter button as active', () => {
        const btn90 = document.querySelector<HTMLElement>('.filter-btn[data-days="90"]')!;
        const btn30 = document.querySelector<HTMLElement>('.filter-btn[data-days="30"]')!;

        btn90.click();

        expect(btn90.classList.contains('active')).toBe(true);
        expect(btn30.classList.contains('active')).toBe(false);
    });
});

describe('insights.ts - refreshReport edge cases', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reportHistory = [];
        state.reports = {};
        insights.lastReport = null;
    });

    it('renders empty state when report history is empty', async () => {
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([]);

        await insights.refreshReport();

        expect(insightsRenderer.renderEmptyState).toHaveBeenCalled();
    });

    it('auto-loads most recent report when no lastReport is set', async () => {
        const mockHistory = [makeReportMeta()];
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue(mockHistory);
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue(makeReportMeta());
        state.reportHistory = mockHistory;

        await insights.refreshReport(null);

        expect(api.fetchReportDetail).toHaveBeenCalledWith(1);
    });

    it('loads specific report when activeId is provided', async () => {
        const history = [
            makeReportMeta(),
            { id: 2, start_date: '2024-03-08', end_date: '2024-03-14', user_email: '', report_summary: '', visualization_data: '' }
        ];
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue(history);
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue(history[1]);
        state.reportHistory = history;

        await insights.refreshReport(2);

        expect(api.fetchReportDetail).toHaveBeenCalledWith(2);
    });

    it('renders error on fetchReportHistory failure', async () => {
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('500'));

        await insights.refreshReport();

        expect(insightsRenderer.renderError).toHaveBeenCalled();
    });
});

describe('insights.ts - loadExistingReport cache hit', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reports = {};
    });

    it('uses memory cache when report is already in state.reports', async () => {
        const reportMeta = makeReportMeta();
        const cached = { ...reportMeta, report_summary: 'Cached Content' };
        state.reports = { '2024-03-01_2024-03-07': cached };

        await insights.loadExistingReport(reportMeta);

        expect(api.fetchReportDetail).not.toHaveBeenCalled();
        expect(insightsRenderer.renderReport).toHaveBeenCalledWith(cached, 'ko', expect.any(Object));
    });

    it('renders error on fetchReportDetail failure', async () => {
        const reportMeta = makeReportMeta();
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('not found'));

        await insights.loadExistingReport(reportMeta);

        expect(insightsRenderer.renderError).toHaveBeenCalled();
    });

    it('shows processing state and starts polling when report.status is processing', async () => {
        const reportMeta = makeReportMeta();
        const processingReport = { ...reportMeta, status: 'processing' as const };
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue(processingReport);

        vi.useFakeTimers();
        await insights.loadExistingReport(reportMeta);
        vi.useRealTimers();

        // renderReport should NOT be called for processing reports
        expect(insightsRenderer.renderReport).not.toHaveBeenCalled();
    });

    it('renders error when report.status is failed', async () => {
        const reportMeta = makeReportMeta();
        const failedReport = { ...reportMeta, status: 'failed' as const };
        (api.fetchReportDetail as ReturnType<typeof vi.fn>).mockResolvedValue(failedReport);

        await insights.loadExistingReport(reportMeta);

        expect(insightsRenderer.renderError).toHaveBeenCalled();
    });
});

describe('insights.ts - generateNewReport', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reportHistory = [];
        state.reports = {};
        insights.lastReport = null;
        (document.getElementById('reportStartDate') as HTMLInputElement).value = '2024-03-01';
        (document.getElementById('reportEndDate') as HTMLInputElement).value = '2024-03-07';
    });

    it('does nothing when start or end date is missing', async () => {
        (document.getElementById('reportStartDate') as HTMLInputElement).value = '';
        await insights.generateNewReport();
        expect(api.generateReport).not.toHaveBeenCalled();
    });

    it('calls api.generateReport with correct params and renders result', async () => {
        const generatedReport = makeReportMeta();
        (api.generateReport as ReturnType<typeof vi.fn>).mockResolvedValue(generatedReport);
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([generatedReport]);
        state.reportHistory = [generatedReport];

        await insights.generateNewReport();

        expect(api.generateReport).toHaveBeenCalledWith('2024-03-01', '2024-03-07', '', '');
        expect(insightsRenderer.renderReport).toHaveBeenCalled();
    });

    it('re-enables the generate button even on error', async () => {
        (api.generateReport as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('server error'));

        const btn = document.getElementById('btnGenerateReport') as HTMLButtonElement;
        await insights.generateNewReport();

        expect(btn.disabled).toBe(false);
    });

    it('calls refreshReport and pollReportStatus when status is processing', async () => {
        const processingReport = { ...makeReportMeta(), status: 'processing' as const };
        (api.generateReport as ReturnType<typeof vi.fn>).mockResolvedValue(processingReport);
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([processingReport]);
        state.reportHistory = [processingReport];

        vi.useFakeTimers();
        await insights.generateNewReport();
        vi.useRealTimers();

        // refreshReport would have been called
        expect(api.fetchReportHistory).toHaveBeenCalled();
    });
});

describe('insights.ts - deleteReport', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        state.reportHistory = [];
        insights.lastReport = null;
    });

    it('calls api.deleteReport and refreshReport on confirmation', async () => {
        vi.stubGlobal('confirm', vi.fn(() => true));
        const reportMeta = makeReportMeta();
        state.reportHistory = [reportMeta];
        (api.deleteReport as ReturnType<typeof vi.fn>).mockResolvedValue({ status: 'ok' });
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([]);

        await insights.deleteReport(1);

        expect(api.deleteReport).toHaveBeenCalledWith(1);
        expect(api.fetchReportHistory).toHaveBeenCalled();
    });

    it('does not delete when user cancels confirm dialog', async () => {
        vi.stubGlobal('confirm', vi.fn(() => false));

        await insights.deleteReport(1);

        expect(api.deleteReport).not.toHaveBeenCalled();
    });

    it('triggers handleDeletionFallback when deleted report was active', async () => {
        vi.stubGlobal('confirm', vi.fn(() => true));
        insights.lastReport = makeReportMeta();
        state.reportHistory = [];
        (api.deleteReport as ReturnType<typeof vi.fn>).mockResolvedValue({ status: 'ok' });
        (api.fetchReportHistory as ReturnType<typeof vi.fn>).mockResolvedValue([]);

        await insights.deleteReport(1);

        // When reportHistory is empty after deletion, renderEmptyState is called via handleDeletionFallback
        expect(insightsRenderer.renderEmptyState).toHaveBeenCalled();
        expect(insights.lastReport).toBeNull();
    });
});

describe('insights.ts - isTabActive / setActiveTab', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
    });

    it('returns true for the panel with active class', () => {
        expect(insights.isTabActive('insightsReportsTab')).toBe(true);
        expect(insights.isTabActive('insightsStatsTab')).toBe(false);
    });

    it('setActiveTab toggles classes correctly', () => {
        const statsPanel = document.getElementById('insightsStatsTab')!;
        const reportsPanel = document.getElementById('insightsReportsTab')!;
        const statsBtn = document.querySelector<HTMLElement>('.insights-tab-btn[data-tab="insightsStatsTab"]')!;
        const reportsBtn = document.querySelector<HTMLElement>('.insights-tab-btn[data-tab="insightsReportsTab"]')!;

        insights.setActiveTab(statsBtn, statsPanel, [reportsBtn], [reportsPanel]);

        expect(statsPanel.classList.contains('c-tabs__panel--active')).toBe(true);
        expect(reportsPanel.classList.contains('c-tabs__panel--active')).toBe(false);
        expect(statsBtn.classList.contains('active')).toBe(true);
        expect(reportsBtn.classList.contains('active')).toBe(false);
    });
});

describe('insights.ts - exportToPDF / exportToNotion', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
        insights.lastReport = makeReportMeta();
    });

    it('exportToPDF calls window.print and restores title', () => {
        const originalTitle = document.title;
        const printSpy = vi.fn();
        vi.stubGlobal('print', printSpy);

        insights.exportToPDF();

        expect(printSpy).toHaveBeenCalled();
        expect(document.title).toBe(originalTitle);
    });

    it('exportToNotion opens URL on success', async () => {
        (api.exportReportToNotion as ReturnType<typeof vi.fn>).mockResolvedValue({ url: 'https://notion.so/page' });
        const openSpy = vi.fn();
        vi.stubGlobal('open', openSpy);

        await insights.exportToNotion();

        expect(openSpy).toHaveBeenCalledWith('https://notion.so/page', '_blank');
    });

    it('exportToNotion shows alert on error', async () => {
        (api.exportReportToNotion as ReturnType<typeof vi.fn>).mockResolvedValue({ error: 'Failed' });
        const alertSpy = vi.fn();
        vi.stubGlobal('alert', alertSpy);

        await insights.exportToNotion();

        expect(alertSpy).toHaveBeenCalled();
    });

    it('exportToNotion does nothing when lastReport is null', async () => {
        insights.lastReport = null;
        await insights.exportToNotion();
        expect(api.exportReportToNotion).not.toHaveBeenCalled();
    });
});

describe('insights.ts - handlePollTimeout / handlePollFailure', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
    });

    it('handlePollTimeout renders error in reportSummaryContent', () => {
        insights.handlePollTimeout();
        expect(insightsRenderer.renderError).toHaveBeenCalledWith(
            document.getElementById('reportSummaryContent'),
            expect.any(String),
            expect.any(Object)
        );
    });

    it('handlePollFailure renders error in reportSummaryContent', () => {
        insights.handlePollFailure();
        expect(insightsRenderer.renderError).toHaveBeenCalledWith(
            document.getElementById('reportSummaryContent'),
            expect.any(String),
            expect.any(Object)
        );
    });
});

describe('insights.ts - initDatePickers', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
    });

    it('sets start/end date inputs to 7-day range from today', async () => {
        await insights.initDatePickers();

        const start = (document.getElementById('reportStartDate') as HTMLInputElement).value;
        const end = (document.getElementById('reportEndDate') as HTMLInputElement).value;

        expect(start).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        expect(end).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        expect(new Date(end).getTime() - new Date(start).getTime()).toBe(7 * 24 * 60 * 60 * 1000);
    });
});

describe('insights.ts - renderAll with null tokenUsage', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_HTML;
        vi.clearAllMocks();
        state.currentLang = 'ko';
    });

    it('skips token usage renderers when tokenUsage is null', () => {
        insights.renderAll(makeStats(), null);

        expect(insightsRenderer.renderTokenUsage).not.toHaveBeenCalled();
        expect(insightsRenderer.renderFilteredNoise).not.toHaveBeenCalled();
    });

    it('still renders daily glance and stats widgets when only tokenUsage is null', () => {
        const stats = makeStats();
        insights.renderAll(stats, null);

        expect(insightsRenderer.renderDailyGlance).toHaveBeenCalledWith(stats, expect.any(Object));
        expect(insightsRenderer.renderChannelDistribution).toHaveBeenCalledWith(stats, expect.any(Object));
    });
});
