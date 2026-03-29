import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insightsRenderer, validateEdges } from './insightsRenderer.js';

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
            <div id="dailyGlance"></div>
            <div id="achievementsList"></div>
            <div id="sourceDistribution"></div>
            <div id="reportList"></div>
            <div id="reportSummaryContent"></div>
            <div id="reportVizChart">
                <div id="reportNetworkChart"></div>
                <div id="reportSankeyChart"></div>
            </div>
            <div id="reportTruncationWarning"></div>
        `;
        vi.clearAllMocks();
    });

    it('should render report list correctly', () => {
        const reports = [
            { id: 1, start_date: '2024-03-01', end_date: '2024-03-07' },
            { id: 2, start_date: '2024-03-08', end_date: '2024-03-14' }
        ];
        insightsRenderer.renderReportList(reports, 1);
        const list = document.getElementById('reportList');
        const items = list.querySelectorAll('.c-report-item');

        expect(items.length).toBe(2);
        expect(items[0].classList.contains('c-report-item--active')).toBe(true);
        expect(items[0].textContent).toContain('2024-03-01');
        expect(list.querySelector('.c-report-item__delete')).not.toBeNull();
    });

    it('should show truncation warning when report is truncated', () => {
        const report = {
            report_summary: 'Truncated summary',
            is_truncated: true
        };
        insightsRenderer.renderReport(report);
        const warning = document.getElementById('reportTruncationWarning');
        expect(warning.classList.contains('u-hidden')).toBe(false);
        expect(warning.innerHTML).toContain('토큰 한도 초과');
    });

    it('should hide truncation warning when report is not truncated', () => {
        const report = {
            report_summary: 'Full summary',
            is_truncated: false
        };
        insightsRenderer.renderReport(report);
        const warning = document.getElementById('reportTruncationWarning');
        expect(warning.classList.contains('u-hidden')).toBe(true);
    });

    it('should render fallback text when report data is missing', () => {
        insightsRenderer.renderReport(null);
        expect(document.getElementById('reportSummaryContent').innerHTML).toContain('생성된 보고서가 없습니다');
        expect(document.getElementById('reportVizChart').innerHTML).toContain('관계망 데이터가 없습니다');
    });

    it('should render markdown summary and initialize network graph when data is valid', () => {
        const reportData = {
            report_summary: '# Weekly Report',
            visualization_data: JSON.stringify({ nodes: [{ id: 'user-a@whatap.io', name: 'User A', value: 10 }], links: [] })
        };

        insightsRenderer.renderReport(reportData);

        expect(marked.parse).toHaveBeenCalledWith('# Weekly Report');
        expect(document.getElementById('reportSummaryContent').innerHTML).toBe('<p># Weekly Report</p>');
        expect(echarts.init).toHaveBeenCalled();
        const instance = echarts.init();
        expect(instance.setOption).toHaveBeenCalled();

        const optionArg = instance.setOption.mock.calls[0][0];
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

    it('should initialize echarts for valid network graph data and configure formatters', () => {
        const container = document.getElementById('reportVizChart');
        const data = {
            nodes: [{ id: 'a@whatap.io', name: 'A', value: 10 }, { id: 'b@whatap.io', name: 'B', value: 5 }],
            links: [{ source: 'a@whatap.io', target: 'b@whatap.io', value: 2 }]
        };
        insightsRenderer.renderNetworkGraph(container, data);

        expect(echarts.init).toHaveBeenCalled();
        const instance = echarts.init();
        expect(instance.setOption).toHaveBeenCalled();

        const optionArg = instance.setOption.mock.calls[0][0];
        expect(optionArg.series[0].type).toBe('graph');
        expect(optionArg.series[0].data.length).toBe(2);
        expect(optionArg.series[0].links.length).toBe(1);

        // Enhance: Test Tooltip Formatter for Network Graph
        const tooltipFormatter = optionArg.tooltip.formatter;

        const edgeParams = {
            dataType: 'edge',
            data: { source: 'a@whatap.io', target: 'b@whatap.io', value: 2 }
        };
        expect(tooltipFormatter(edgeParams)).toContain('A ↔ B');

        const nodeParams = {
            dataType: 'node',
            data: { id: 'a@whatap.io', name: 'A', value: 10 }
        };
        expect(tooltipFormatter(nodeParams)).toContain('A');
        expect(tooltipFormatter(nodeParams)).toContain('10');
    });
});

describe('insightsRenderer.js - Utilities & Sankey Logic', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // Clear console error explicitly for clean test output
        vi.spyOn(console, 'error').mockImplementation(() => { });
    });

    it('validateEdges should remove links with missing nodes', () => {
        const nodes = [{ id: 'a@whatap.io' }, { id: 'b@whatap.io' }];
        const links = [
            { source: 'a@whatap.io', target: 'b@whatap.io', value: 1 },
            { source: 'a@whatap.io', target: 'missing@whatap.io', value: 2 }
        ];

        const validLinks = validateEdges(nodes, links);

        expect(validLinks.length).toBe(1);
        expect(validLinks[0].target).toBe('b@whatap.io');
        expect(console.error).toHaveBeenCalled();
    });

    it('renderSankeyChart should merge bidirectional edges into a DAG and ignore self-loops', () => {
        const container = document.createElement('div');
        const data = {
            nodes: [
                { id: 'alice@whatap.io', name: 'Alice', is_me: true, category: 'Internal' },
                { id: 'bob@whatap.io', name: 'Bob', category: 'Internal' }
            ],
            links: [
                { source: 'alice@whatap.io', target: 'bob@whatap.io', value: 3 },
                { source: 'bob@whatap.io', target: 'alice@whatap.io', value: 2 },
                { source: 'alice@whatap.io', target: 'alice@whatap.io', value: 10 } // self-loop
            ]
        };

        insightsRenderer.renderSankeyChart(container, data);

        expect(echarts.init().setOption).toHaveBeenCalled();
        const optionArg = echarts.init().setOption.mock.calls[0][0];
        const series = optionArg.series[0];

        expect(series.type).toBe('sankey');

        // Assert bi-directional merge logic (3 + 2 = 5)
        expect(series.links.length).toBe(1);
        expect(series.links[0].value).toBe(5);
        expect(series.links[0].source).toBe('alice@whatap.io');
        expect(series.links[0].target).toBe('bob@whatap.io');

        // Assert node 1:1 format mappings
        expect(series.data[0].id).toBe('alice@whatap.io');
        expect(series.data[0].name).toBe('alice@whatap.io');
        expect(series.data[0].alias).toBe('Alice');

        // Enhance: Assert visual properties based on node metadata.
        // This verifies that the renderer correctly applies styles for 'me' and 'internal' nodes.
        const meNode = series.data.find(n => n.id === 'alice@whatap.io');
        const internalNode = series.data.find(n => n.id === 'bob@whatap.io');

        // Assuming the renderer sets itemStyle.color based on a predefined mapping.
        expect(meNode.itemStyle.color).toBeDefined();
        expect(internalNode.itemStyle.color).toBeDefined();

        // Enhance: Test Tooltip Formatter for Sankey Chart
        const tooltipFormatter = optionArg.tooltip.formatter;

        const edgeParams = {
            dataType: 'edge',
            data: { source: 'alice@whatap.io', target: 'bob@whatap.io', value: 5 }
        };
        const edgeTooltip = tooltipFormatter(edgeParams);
        expect(edgeTooltip).toContain('Alice ↔ Bob');
        expect(edgeTooltip).toContain('5');

        const nodeParams = {
            dataType: 'node',
            data: { id: 'alice@whatap.io', name: 'alice@whatap.io', alias: 'Alice' },
            value: 12
        };
        const nodeTooltip = tooltipFormatter(nodeParams);
        expect(nodeTooltip).toContain('Alice:');
        expect(nodeTooltip).toContain('12');
    });
});
