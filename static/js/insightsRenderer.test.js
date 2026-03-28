import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insightsRenderer } from './insightsRenderer.js';

// Mock globals for ECharts and Marked
const mockSetOption = vi.fn();
const mockOn = vi.fn();
const mockOff = vi.fn();
const mockGetZr = vi.fn(() => ({ off: vi.fn(), on: vi.fn() }));
const mockResize = vi.fn();

vi.stubGlobal('echarts', {
    getInstanceByDom: vi.fn(() => null),
    init: vi.fn(() => ({
        setOption: mockSetOption,
        on: mockOn,
        off: mockOff,
        getZr: mockGetZr,
        resize: mockResize
    }))
});

vi.stubGlobal('marked', {
    parse: vi.fn((text) => `<p>${text}</p>`)
});

describe('insightsRenderer.js', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="dailyGlance"></div>
            <div id="achievementsList"></div>
            <div id="sourceDistribution"></div>
        `;
    });

    it('should render daily glance correctly', () => {
        vi.spyOn(Math, 'random').mockReturnValue(0);
        const data = { total_completed: 42, peak_time: '14:00', abandoned_tasks: 0 };
        insightsRenderer.renderDailyGlance(data);

        const glance = document.getElementById('dailyGlance');
        expect(glance.textContent).toContain('42');
        expect(glance.textContent).toContain('14:00');
        expect(glance.textContent).toContain('원활하게');
    });

    it('should show warning in glance when abandoned tasks exist', () => {
        const data = { total_completed: 10, peak_time: '10:00', abandoned_tasks: 5 };
        insightsRenderer.renderDailyGlance(data, 'ko');
        const glance = document.getElementById('dailyGlance').textContent;
        expect(glance).toContain('⚠️');
        expect(glance).toContain('5');
    });

    it('should consolidate Task Master series into one card', () => {
        const all = [
            { id: 'tm1', name: '첫 걸음', target_value: 1, icon: '🌱' },
            { id: 'tm2', name: '태스크 마스터 I', target_value: 10, icon: '🏅' },
            { id: 'tm3', name: '태스크 마스터 II', target_value: 50, icon: '🎖️' }
        ];
        // User has unlocked Tier I (첫 걸음 and 태스크 마스터 I)
        const user = [{ achievement_id: 'tm1' }, { achievement_id: 'tm2' }];

        insightsRenderer.renderAchievements(all, user, { total_completed: 15 });
        const list = document.getElementById('achievementsList');
        const items = list.querySelectorAll('.c-achievement');

        // Should only show '태스크 마스터 II' as the current target (first locked one)
        // because 첫 걸음 and 태스크 마스터 I are in the same series and unlocked.
        // Wait, current logic: first locked one, or last unlocked one.
        // tm1, tm2 are unlocked. tm3 is locked. So tm3 is the representative.
        expect(items.length).toBe(1);
        expect(items[0].textContent).toContain('태스크 마스터 II');
    });

    it('should limit initial visible achievements to 3 and show toggle button', () => {
        const all = [
            { id: '1', name: 'A1', target_value: 10 },
            { id: '2', name: 'A2', target_value: 10 },
            { id: '3', name: 'A3', target_value: 10 },
            { id: '4', name: 'A4', target_value: 10 },
            { id: '5', name: 'A5', target_value: 10 }
        ];
        insightsRenderer.renderAchievements(all, [], {});

        const list = document.getElementById('achievementsList');
        const visibleItems = Array.from(list.querySelectorAll('.c-achievement'))
            .filter(item => item.style.display !== 'none');

        expect(visibleItems.length).toBe(3);
        expect(document.getElementById('btnShowMoreAch')).not.toBeNull();
    });

    it('should render source distribution chart items', () => {
        const dist = { slack: 50, whatsapp: 50 };
        insightsRenderer.renderSourceDistribution({ source_distribution: dist, source_distribution_total: dist });
        const chart = document.getElementById('sourceDistribution');
        expect(chart.querySelectorAll('.c-stacked-bar__segment').length).toBe(4); // 2 bars * 2 segments
        expect(chart.innerHTML).toContain('50%');
    });
});

describe('insightsRenderer.js - renderReport & Network Graph', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="reportSummaryContent"></div>
            <div id="reportVizChart"></div>
        `;
        vi.clearAllMocks();
    });

    it('should render fallback text when report data is missing', () => {
        insightsRenderer.renderReport(null);
        expect(document.getElementById('reportSummaryContent').innerHTML).toContain('요약된 보고서가 아직 없습니다');
        expect(document.getElementById('reportVizChart').innerHTML).toContain('관계망 데이터가 없습니다');
    });

    it('should render markdown summary and initialize network graph when data is valid', () => {
        const reportData = {
            report_summary: '# Weekly Report',
            visualization_data: JSON.stringify({ nodes: [{ name: 'User A', value: 10 }], links: [] })
        };

        insightsRenderer.renderReport(reportData);

        expect(marked.parse).toHaveBeenCalledWith('# Weekly Report');
        expect(document.getElementById('reportSummaryContent').innerHTML).toBe('<p># Weekly Report</p>');
        expect(echarts.init).toHaveBeenCalled();
        expect(mockSetOption).toHaveBeenCalled();

        const optionArg = mockSetOption.mock.calls[0][0];
        expect(optionArg.series[0].data[0].name).toBe('User A');
    });

    it('should gracefully handle invalid JSON in visualization data', () => {
        const reportData = {
            report_summary: 'Valid Markdown',
            visualization_data: 'Invalid JSON {['
        };
        insightsRenderer.renderReport(reportData);
        expect(document.getElementById('reportVizChart').innerHTML).toContain('시각화 데이터를 처리하지 못했습니다');
    });
});
