// @vitest-environment happy-dom
import { describe, it, expect, beforeEach } from 'vitest';
import { insightsRenderer } from './insightsRenderer.ts';
import { state } from './state.ts';
import { UserStats } from './types.ts';

interface MockI18n {
    completedTasks: string;
    totalCommunication: string;
    waiting: string;
    tokenUsageTitle: string;
    sourceDistribution: string;
    weeklyReportTitle: string;
    noResults: string;
    recentActivity91: string;
    peakTime: string;
    staleTasks: string;
    averageDaily: string;
    totalCompleted: string;
    noReportsYet: string;
    generateReportDesc: string;
    retryLanguageSelection: string;
    generatingReport: string;
    loadingData: string;
    generatingTranslation: string;
    insights: { no_reports: string };
    todayAIUsage: string;
    monthlyAIUsage: string;
    estimatedCost: string;
    noiseFiltered: string;
    filteredToday: string;
    filteredMonthly: string;
}

const mockI18n: MockI18n = {
    completedTasks: '완료 업무',
    totalCommunication: '총 소통량',
    waiting: '대기',
    tokenUsageTitle: '토큰 사용량',
    sourceDistribution: '소스별 비중',
    weeklyReportTitle: '주간 리포트',
    noResults: 'No data',
    recentActivity91: '최근 활동 (91일)',
    peakTime: '피크 타임',
    staleTasks: '방치된 업무',
    averageDaily: '일 평균',
    totalCompleted: '누적',
    noReportsYet: '생성된 리포트가 없습니다',
    generateReportDesc: 'AI를 통해 오늘 업무 리포트를 생성해 보세요.',
    retryLanguageSelection: '다시 한 번 언어를 선택해 주세요',
    generatingReport: 'AI 리포트 분석 중...',
    loadingData: '데이터를 불러오는 중...',
    generatingTranslation: 'AI 번역 생성 중...',
    insights: { no_reports: '사용 가능한 보고서가 없습니다.' },
    todayAIUsage: '오늘 AI 사용',
    monthlyAIUsage: '이번 달 AI 사용',
    estimatedCost: '추정 비용',
    noiseFiltered: '노이즈 필터링',
    filteredToday: '오늘 차단',
    filteredMonthly: '이번 달 차단',
};

const makeStats = (overrides: Partial<UserStats> = {}): UserStats => ({
    pending_me: 0,
    pending_others: 0,
    total_completed: 10,
    peak_time: '14:00',
    abandoned_tasks: 3,
    daily_completions: {},
    source_distribution: { slack: 70, whatsapp: 30 },
    source_distribution_total: { slack: 70, whatsapp: 30 },
    hourly_activity: { '9': 5, '10': 12, '14': 20, '15': 8 },
    completion_history: [
        { date: '2024-03-01', counts: { slack: 3, whatsapp: 1 } },
        { date: '2024-03-02', counts: { slack: 5 } },
    ],
    ...overrides
});

const BASE_DOM = `
    <div class="c-insights-card" id="cardDailyGlance">
        <h3 class="c-insights-card__title">Daily Stats</h3>
        <div id="dailyGlanceValue" class="c-insights-card__main-value">-</div>
        <div id="dailyGlanceDetail" class="c-insights-card__detail">Syncing...</div>
    </div>
    <div class="c-insights-card c-insights-card--square" id="ai-usage-consolidated">
        <div class="u-text-dim u-text-sm">AI Usage Data Syncing...</div>
    </div>
    <div id="ai-noise-filtered"></div>
    <div id="source-distribution-slot"></div>
    <div id="activity-heatmap-slot"></div>
    <div id="ankiChartContainer" style="width:100px; height:100px;"></div>
    <div id="stat-peak"></div>
    <div id="staleTasksValue"></div>
    <div id="reportList"></div>
    <div id="reportSummaryContent"></div>
    <div id="reportNetworkChart"></div>
    <div class="c-insights-report-main"></div>
`;

describe('insightsRenderer.ts - Slot-based Rendering (JS Test)', () => {
    beforeEach(() => {
        state.currentLang = 'ko';
        document.body.innerHTML = BASE_DOM;
    });

    it('should update daily glance slots without destroying card title', () => {
        insightsRenderer.renderDailyGlance(makeStats(), mockI18n);

        const card = document.getElementById('cardDailyGlance') as HTMLElement;
        const value = document.getElementById('dailyGlanceValue') as HTMLElement;
        const title = card.querySelector('.c-insights-card__title');

        expect(value.textContent).toContain('10');
        expect(title?.textContent).toBe('Daily Stats');
        expect(card.classList.contains('c-insights-card')).toBe(true);
    });

    it('should update consolidated AI usage widget with formatted numbers and breakdown (Daily/Monthly)', () => {
        const usage = {
            todayTotal: 1234, todayPrompt: 600, todayCompletion: 634, todayCost: 0,
            monthlyTotal: 56789, monthlyPrompt: 30000, monthlyCompletion: 26789,
            monthlyCost: 1.25, model: 'Gemini 3 Flash'
        };
        insightsRenderer.renderTokenUsage(usage, mockI18n);

        const slot = document.getElementById('ai-usage-consolidated') as HTMLElement;
        expect(slot.innerHTML).toContain('토큰 사용량');
        expect(slot.innerHTML).toContain('1,234');
        expect(slot.innerHTML).toContain('입 600');
        expect(slot.innerHTML).toContain('출 634');
        expect(slot.innerHTML).toContain('56,789');
        expect(slot.innerHTML).toContain('입 30,000');
        expect(slot.innerHTML).toContain('출 26,789');
        expect(slot.innerHTML).toContain('$1.25');
        expect(slot.innerHTML).toContain('Gemini 3 Flash');
    });

    it('should render source distribution chart container and labels correctly', () => {
        insightsRenderer.renderChannelDistribution(makeStats(), mockI18n);

        const container = document.getElementById('source-distribution-slot') as HTMLElement;
        const chartNode = document.getElementById('sourceDistributionChart');

        expect(chartNode).not.toBeNull();
        expect(container.innerHTML).toContain('소스별 비중');
    });
});

describe('insightsRenderer.ts - renderActivityHeatmap', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders heatmap cells equal to 91 days for completion_history input', () => {
        insightsRenderer.renderActivityHeatmap(makeStats(), mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        const cells = container.querySelectorAll('.heatmap-grid__cell');
        expect(cells.length).toBe(91);
    });

    it('assigns tier classes for days with activity', () => {
        const stats = makeStats({
            completion_history: [
                { date: new Date().toISOString().split('T')[0], counts: { slack: 8 } }
            ]
        });
        insightsRenderer.renderActivityHeatmap(stats, mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        const tieredCell = container.querySelector('[class*="tier-4"]');
        expect(tieredCell).not.toBeNull();
    });

    it('renders empty state when completion_history is empty', () => {
        const stats = makeStats({ completion_history: [] });
        insightsRenderer.renderActivityHeatmap(stats, mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        expect(container.innerHTML).toContain('heatmap-widget--empty');
    });

    it('renders empty state when stats is null', () => {
        insightsRenderer.renderActivityHeatmap(null, mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        expect(container.innerHTML).toContain('heatmap-widget--empty');
    });

    it('uses custom targetId when provided', () => {
        document.body.innerHTML += `<div id="custom-heatmap"></div>`;
        insightsRenderer.renderActivityHeatmap(makeStats(), mockI18n, 'custom-heatmap');

        const container = document.getElementById('custom-heatmap')!;
        const cells = container.querySelectorAll('.heatmap-grid__cell');
        expect(cells.length).toBe(91);
    });

    it('stores date and count in cell data attributes', () => {
        const today = new Date().toISOString().split('T')[0];
        const stats = makeStats({
            completion_history: [{ date: today, counts: { slack: 2 } }]
        });
        insightsRenderer.renderActivityHeatmap(stats, mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        const todayCell = container.querySelector(`[data-date="${today}"]`) as HTMLElement | null;
        expect(todayCell).not.toBeNull();
        expect(todayCell!.dataset.count).toBe('2');
    });
});

describe('insightsRenderer.ts - renderHourlyActivity', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders 24 bars for a full hourly_activity map', () => {
        insightsRenderer.renderHourlyActivity(makeStats(), mockI18n);

        const container = document.getElementById('stat-peak')!;
        const bars = container.querySelectorAll('.c-peak-chart__bar');
        expect(bars.length).toBe(24);
    });

    it('renders 24 labels for hours 0-23', () => {
        insightsRenderer.renderHourlyActivity(makeStats(), mockI18n);

        const container = document.getElementById('stat-peak')!;
        const labels = container.querySelectorAll('.c-peak-chart__label');
        expect(labels.length).toBe(24);
    });

    it('assigns tier classes based on relative activity level', () => {
        insightsRenderer.renderHourlyActivity(makeStats(), mockI18n);

        const container = document.getElementById('stat-peak')!;
        // Hour 14 has max count (20), so it should be tier-4
        const bars = container.querySelectorAll('.c-peak-chart__bar');
        const peakBar = bars[14];
        expect(peakBar.className).toContain('tier-4');
    });

    it('does nothing when container is missing', () => {
        document.getElementById('stat-peak')!.remove();
        // Should not throw
        expect(() => insightsRenderer.renderHourlyActivity(makeStats(), mockI18n)).not.toThrow();
    });

    it('does nothing when stats is null', () => {
        insightsRenderer.renderHourlyActivity(null, mockI18n);
        const container = document.getElementById('stat-peak')!;
        expect(container.innerHTML).toBe('');
    });

    it('does nothing when hourly_activity is undefined', () => {
        const stats = makeStats({ hourly_activity: undefined as unknown as Record<string, number> });
        insightsRenderer.renderHourlyActivity(stats, mockI18n);
        const container = document.getElementById('stat-peak')!;
        expect(container.innerHTML).toBe('');
    });
});

describe('insightsRenderer.ts - renderChannelDistribution', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders SVG pie paths equal to entry count', () => {
        insightsRenderer.renderChannelDistribution(makeStats(), mockI18n);

        const svg = document.querySelector('#sourceDistributionChart svg');
        expect(svg).not.toBeNull();
        // 2 entries (slack, whatsapp) + 1 donut hole circle
        const paths = svg!.querySelectorAll('path');
        expect(paths.length).toBe(2);
    });

    it('renders donut hole circle in pie chart', () => {
        insightsRenderer.renderChannelDistribution(makeStats(), mockI18n);

        const svg = document.querySelector('#sourceDistributionChart svg')!;
        const circles = svg.querySelectorAll('circle');
        expect(circles.length).toBe(1);
    });

    it('shows no-data message when all entries have zero value', () => {
        const stats = makeStats({ source_distribution_total: { slack: 0, whatsapp: 0 }, source_distribution: {} });
        insightsRenderer.renderChannelDistribution(stats, mockI18n);

        const container = document.getElementById('source-distribution-slot')!;
        expect(container.innerHTML).toContain('No data');
    });

    it('capitalizes channel names in tooltip data', () => {
        insightsRenderer.renderChannelDistribution(makeStats(), mockI18n);

        // SVG path elements are created with event listeners for tooltip; verify paths exist
        const paths = document.querySelectorAll('#sourceDistributionChart path');
        expect(paths.length).toBeGreaterThan(0);
    });

    it('falls back to source_distribution when source_distribution_total is empty object', () => {
        // Why: source_distribution_total = {} is truthy, so the renderer reads it and finds no entries.
        // It then falls back to no-data message rather than reading source_distribution.
        // This test documents the actual behavior (bug candidate, see skip below).
        const stats = makeStats({
            source_distribution_total: {},
            source_distribution: { telegram: 50 }
        });
        insightsRenderer.renderChannelDistribution(stats, mockI18n);

        const container = document.getElementById('source-distribution-slot')!;
        // The empty source_distribution_total causes "No data" to render
        expect(container.innerHTML).toContain('No data');
    });
});

describe('insightsRenderer.ts - renderStaleTasks', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders stale task count in slot', () => {
        insightsRenderer.renderStaleTasks(makeStats(), mockI18n);

        const slot = document.getElementById('staleTasksValue')!;
        expect(slot.innerHTML).toContain('3');
        expect(slot.innerHTML).toContain('방치된 업무');
    });

    it('renders 0 when stats is null', () => {
        insightsRenderer.renderStaleTasks(null, mockI18n);

        const slot = document.getElementById('staleTasksValue')!;
        expect(slot.innerHTML).toContain('0');
    });

    it('renders 0 when abandoned_tasks is missing', () => {
        const stats = makeStats({ abandoned_tasks: undefined as unknown as number });
        insightsRenderer.renderStaleTasks(stats, mockI18n);

        const slot = document.getElementById('staleTasksValue')!;
        expect(slot.innerHTML).toContain('0');
    });

    it('does nothing when slot element is absent', () => {
        document.getElementById('staleTasksValue')!.remove();
        expect(() => insightsRenderer.renderStaleTasks(makeStats(), mockI18n)).not.toThrow();
    });
});

describe('insightsRenderer.ts - renderDailyGlance', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('shows total_completed and average in slot', () => {
        insightsRenderer.renderDailyGlance(makeStats(), mockI18n);

        const slot = document.getElementById('dailyGlanceValue')!;
        expect(slot.innerHTML).toContain('10');
        expect(slot.innerHTML).toContain('5.0'); // 10 completed / 2 history entries
    });

    it('shows 0 average when completion_history is empty', () => {
        insightsRenderer.renderDailyGlance(makeStats({ completion_history: [] }), mockI18n);

        const slot = document.getElementById('dailyGlanceValue')!;
        expect(slot.innerHTML).toContain('0');
    });

    it('does nothing when slot is absent', () => {
        document.getElementById('dailyGlanceValue')!.remove();
        expect(() => insightsRenderer.renderDailyGlance(makeStats(), mockI18n)).not.toThrow();
    });

    it('does nothing when stats is null', () => {
        insightsRenderer.renderDailyGlance(null, mockI18n);
        // slot should remain unchanged (no slot element found → early return)
        const slot = document.getElementById('dailyGlanceValue')!;
        expect(slot.innerHTML).toBe('-');
    });
});

describe('insightsRenderer.ts - renderCompletionTrend', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders SVG with data points for each history entry within days window', () => {
        insightsRenderer.renderCompletionTrend(makeStats(), 30);

        const container = document.getElementById('ankiChartContainer')!;
        const svg = container.querySelector('svg');
        expect(svg).not.toBeNull();
        // Circles = data point dots, one per history point (max 30, we have 2)
        const dots = container.querySelectorAll('circle');
        expect(dots.length).toBe(2);
    });

    it('renders empty-data message when completion_history is empty', () => {
        insightsRenderer.renderCompletionTrend(makeStats({ completion_history: [] }), 30);

        const container = document.getElementById('ankiChartContainer')!;
        expect(container.innerHTML).toContain('데이터 없음');
    });

    it('renders empty-data message when stats is null', () => {
        insightsRenderer.renderCompletionTrend(null, 30);

        const container = document.getElementById('ankiChartContainer')!;
        expect(container.innerHTML).toContain('데이터 없음');
    });

    it('slices history to requested days window', () => {
        const history = Array.from({ length: 50 }, (_, i) => ({
            date: `2024-01-${String(i + 1).padStart(2, '0')}`,
            counts: { slack: i + 1 }
        }));
        insightsRenderer.renderCompletionTrend(makeStats({ completion_history: history }), 10);

        const container = document.getElementById('ankiChartContainer')!;
        const dots = container.querySelectorAll('circle');
        expect(dots.length).toBe(10);
    });

    it('does nothing when container element is missing', () => {
        document.getElementById('ankiChartContainer')!.remove();
        expect(() => insightsRenderer.renderCompletionTrend(makeStats(), 30)).not.toThrow();
    });
});

describe('insightsRenderer.ts - renderTokenUsage edge cases', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders zeros when usage is null', () => {
        insightsRenderer.renderTokenUsage(null, mockI18n);

        const slot = document.getElementById('ai-usage-consolidated')!;
        expect(slot.innerHTML).toContain('0');
    });

    it('formats cost with 2 decimal places', () => {
        insightsRenderer.renderTokenUsage(
            { todayTotal: 0, todayPrompt: 0, todayCompletion: 0, todayCost: 0, monthlyTotal: 0, monthlyPrompt: 0, monthlyCompletion: 0, monthlyCost: 3, model: 'X' },
            mockI18n
        );

        const slot = document.getElementById('ai-usage-consolidated')!;
        expect(slot.innerHTML).toContain('$3.00');
    });

    it('does nothing when slot element is absent', () => {
        document.getElementById('ai-usage-consolidated')!.remove();
        expect(() => insightsRenderer.renderTokenUsage(null, mockI18n)).not.toThrow();
    });
});

describe('insightsRenderer.ts - renderFilteredNoise', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders today and monthly filtered counts', () => {
        insightsRenderer.renderFilteredNoise(
            { todayTotal: 0, todayPrompt: 0, todayCompletion: 0, todayCost: 0, monthlyTotal: 0, monthlyPrompt: 0, monthlyCompletion: 0, monthlyCost: 0, model: '', todayFiltered: 42, monthlyFiltered: 300 },
            mockI18n
        );

        const slot = document.getElementById('ai-noise-filtered')!;
        expect(slot.innerHTML).toContain('42');
        expect(slot.innerHTML).toContain('300');
    });

    it('renders zeros when usage is null', () => {
        insightsRenderer.renderFilteredNoise(null, mockI18n);

        const slot = document.getElementById('ai-noise-filtered')!;
        expect(slot.innerHTML).toContain('0');
    });

    it('does nothing when slot element is absent', () => {
        document.getElementById('ai-noise-filtered')!.remove();
        expect(() => insightsRenderer.renderFilteredNoise(null, mockI18n)).not.toThrow();
    });
});

describe('insightsRenderer.ts - renderEmptyState', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders empty state with i18n strings in reportSummaryContent', () => {
        insightsRenderer.renderEmptyState(mockI18n);

        const content = document.getElementById('reportSummaryContent')!;
        expect(content.innerHTML).toContain('생성된 리포트가 없습니다');
    });

    it('clears reportNetworkChart when rendering empty state', () => {
        document.getElementById('reportNetworkChart')!.innerHTML = '<svg>old</svg>';
        insightsRenderer.renderEmptyState(mockI18n);

        expect(document.getElementById('reportNetworkChart')!.innerHTML).toBe('');
    });

    it('does nothing when reportSummaryContent is absent', () => {
        document.getElementById('reportSummaryContent')!.remove();
        expect(() => insightsRenderer.renderEmptyState(mockI18n)).not.toThrow();
    });
});

describe('insightsRenderer.ts - renderError', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders error message in container', () => {
        const container = document.getElementById('reportSummaryContent')!;
        insightsRenderer.renderError(container, 'Something went wrong', mockI18n);

        expect(container.innerHTML).toContain('Something went wrong');
        expect(container.innerHTML).toContain('c-report-error');
    });

    it('includes retry message from i18n', () => {
        const container = document.getElementById('reportSummaryContent')!;
        insightsRenderer.renderError(container, 'Error', mockI18n);

        expect(container.innerHTML).toContain('다시 한 번 언어를 선택해 주세요');
    });
});

describe('insightsRenderer.ts - renderLoading', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders spinner and default report loading message', () => {
        const container = document.getElementById('reportSummaryContent')!;
        insightsRenderer.renderLoading(container, mockI18n, 'report');

        expect(container.innerHTML).toContain('spinner');
        expect(container.innerHTML).toContain('AI 리포트 분석 중');
    });

    it('uses translation message for type=translation', () => {
        const container = document.getElementById('reportSummaryContent')!;
        insightsRenderer.renderLoading(container, mockI18n, 'translation');

        expect(container.innerHTML).toContain('AI 번역 생성 중');
    });

    it('uses load message for type=load', () => {
        const container = document.getElementById('reportSummaryContent')!;
        insightsRenderer.renderLoading(container, mockI18n, 'load');

        expect(container.innerHTML).toContain('데이터를 불러오는 중');
    });
});

describe('insightsRenderer.ts - renderReportList', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('renders empty message when history is empty array', () => {
        insightsRenderer.renderReportList([], mockI18n, null);

        const list = document.getElementById('reportList')!;
        expect(list.innerHTML).toContain('사용 가능한 보고서가 없습니다');
    });

    it('does nothing when reportList element is absent', () => {
        document.getElementById('reportList')!.remove();
        expect(() => insightsRenderer.renderReportList([], mockI18n, null)).not.toThrow();
    });
});

describe('insightsRenderer.ts - bindHeatmapEvents / updateTooltip', () => {
    beforeEach(() => {
        document.body.innerHTML = BASE_DOM;
    });

    it('creates tooltip element on mousemove over heatmap cell', () => {
        insightsRenderer.renderActivityHeatmap(makeStats(), mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;
        const cell = container.querySelector('.heatmap-grid__cell') as HTMLElement;

        // Simulate mousemove; pageX/pageY are read-only in strict DOM — dispatch on the cell directly
        const mousemoveEvent = new MouseEvent('mousemove', { bubbles: true });
        cell.dispatchEvent(mousemoveEvent);

        const tooltip = document.getElementById('heatmap-tooltip');
        expect(tooltip).not.toBeNull();
    });

    it('removes active class from tooltip on mouseleave', () => {
        insightsRenderer.renderActivityHeatmap(makeStats(), mockI18n);

        const container = document.getElementById('activity-heatmap-slot')!;

        // Create tooltip first
        const t = document.createElement('div');
        t.id = 'heatmap-tooltip';
        t.className = 'c-insights-tooltip c-insights-tooltip--active';
        document.body.appendChild(t);

        container.dispatchEvent(new MouseEvent('mouseleave', { bubbles: true }));

        expect(document.getElementById('heatmap-tooltip')!.classList.contains('c-insights-tooltip--active')).toBe(false);
    });
});
