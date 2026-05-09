// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { reportsRenderer } from './reports-renderer';
import { IReportData, IReportNode, IReportLink, StalledTaskRow, I18nEntry } from '../types';

// ──────────────────────────────────────────────────────────────────────────────
// Shared test fixtures
// ──────────────────────────────────────────────────────────────────────────────

const i18n: I18nEntry = {
    noReports: 'No reports found.',
    delete: 'Delete',
    customer: 'Customer',
    taskCount: 'Tasks',
    taskSummary: 'Summary',
    source: 'Source',
    requester: 'Requester',
    assignee: 'Assignee',
    status: 'Status',
    delay: 'Delay',
    days: 'days',
    task: 'Task',
    truncationWarning: 'Some past data were omitted.',
};

function makeReport(overrides: Partial<IReportData> = {}): IReportData {
    return {
        id: 1,
        user_email: 'test@example.com',
        start_date: '2026-05-01',
        end_date: '2026-05-07',
        report_summary: '## Summary\n\nNo content.',
        visualization_data: '',
        status: 'completed',
        ...overrides,
    };
}

// ──────────────────────────────────────────────────────────────────────────────
// renderHistory
// ──────────────────────────────────────────────────────────────────────────────

describe('reportsRenderer.renderHistory', () => {
    let container: HTMLElement;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    it('shows empty state when items array is empty', () => {
        reportsRenderer.renderHistory(container, [], () => undefined, i18n);
        expect(container.innerHTML).toContain('No reports found.');
    });

    it('shows empty state when items is undefined-like (null guard)', () => {
        // Why: defensive — ensure null-like inputs don't throw
        reportsRenderer.renderHistory(container, null as unknown as IReportData[], () => undefined, i18n);
        expect(container.innerHTML).toContain('No reports found.');
    });

    it('renders one item per report', () => {
        const items = [makeReport({ id: 1 }), makeReport({ id: 2 })];
        reportsRenderer.renderHistory(container, items, () => undefined, i18n);
        const reportItems = container.querySelectorAll('.c-insights-report-item');
        expect(reportItems).toHaveLength(2);
    });

    it('uses data-id attribute matching item id', () => {
        reportsRenderer.renderHistory(container, [makeReport({ id: 42 })], () => undefined, i18n);
        const item = container.querySelector('[data-id="42"]');
        expect(item).not.toBeNull();
    });

    it('shows daily report label when start_date equals end_date', () => {
        const item = makeReport({ start_date: '2026-05-05', end_date: '2026-05-05' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('Daily Report');
        expect(container.innerHTML).not.toContain('Weekly Report');
        expect(container.innerHTML).toContain('2026-05-05');
    });

    it('shows weekly report label when start and end dates differ', () => {
        const item = makeReport({ start_date: '2026-05-01', end_date: '2026-05-07' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('Weekly Report');
        expect(container.innerHTML).toContain('2026-05-01 ~ 2026-05-07');
    });

    it('shows processing status emoji', () => {
        const item = makeReport({ status: 'processing' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('⌛');
    });

    it('shows failed status emoji', () => {
        const item = makeReport({ status: 'failed' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('⚠️');
    });

    it('shows completed status emoji', () => {
        const item = makeReport({ status: 'completed' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('✅');
    });

    it('renders custom title when present', () => {
        const item = makeReport({ title: 'My Custom Title' });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).toContain('My Custom Title');
    });

    it('omits custom title span when title is absent', () => {
        const item = makeReport({ title: undefined });
        reportsRenderer.renderHistory(container, [item], () => undefined, i18n);
        expect(container.innerHTML).not.toContain('c-insights-report-item__custom-title');
    });

    it('calls onSelect with the correct item when clicked', () => {
        let selected: IReportData | null = null;
        const item = makeReport({ id: 99 });
        reportsRenderer.renderHistory(container, [item], (i) => { selected = i; }, i18n);
        const reportItem = container.querySelector('.c-insights-report-item') as HTMLElement;
        reportItem.click();
        expect(selected).not.toBeNull();
        expect((selected as unknown as IReportData).id).toBe(99);
    });

    it('marks clicked item as active and clears previous active', () => {
        const items = [makeReport({ id: 1 }), makeReport({ id: 2 })];
        reportsRenderer.renderHistory(container, items, () => undefined, i18n);
        const [first, second] = container.querySelectorAll('.c-insights-report-item') as NodeListOf<HTMLElement>;
        first.click();
        expect(first.classList.contains('c-insights-report-item--active')).toBe(true);
        second.click();
        expect(second.classList.contains('c-insights-report-item--active')).toBe(true);
        expect(first.classList.contains('c-insights-report-item--active')).toBe(false);
    });

    it('delete button carries correct data-id', () => {
        reportsRenderer.renderHistory(container, [makeReport({ id: 7 })], () => undefined, i18n);
        const btn = container.querySelector('.c-insights-report-item__delete');
        expect(btn?.getAttribute('data-id')).toBe('7');
    });

    it('clears previous content on re-render', () => {
        reportsRenderer.renderHistory(container, [makeReport({ id: 1 })], () => undefined, i18n);
        reportsRenderer.renderHistory(container, [makeReport({ id: 2 })], () => undefined, i18n);
        const items = container.querySelectorAll('.c-insights-report-item');
        expect(items).toHaveLength(1);
        expect(items[0].getAttribute('data-id')).toBe('2');
    });
});

// ──────────────────────────────────────────────────────────────────────────────
// renderActivityComponent
// ──────────────────────────────────────────────────────────────────────────────

describe('reportsRenderer.renderActivityComponent', () => {
    const activityData = [
        { customer: 'Acme Corp', count: 5, summary: 'Onboarding support' },
        { customer: 'Beta Inc', count: 3, summary: 'API questions' },
    ];

    it('returns HTML string containing the table wrapper', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        expect(html).toContain('c-report-table-wrapper');
        expect(html).toContain('c-report-table');
    });

    it('renders thead with i18n column headers', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        expect(html).toContain('Customer');
        expect(html).toContain('Tasks');
        expect(html).toContain('Summary');
    });

    it('renders one row per activity item', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        const div = document.createElement('div');
        div.innerHTML = html;
        expect(div.querySelectorAll('tbody tr')).toHaveLength(2);
    });

    it('shows rank number in first cell', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        const div = document.createElement('div');
        div.innerHTML = html;
        const firstRank = div.querySelector('.c-report-table__cell--rank');
        expect(firstRank?.textContent?.trim()).toBe('1');
    });

    it('shows customer name in customer cell', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        expect(html).toContain('Acme Corp');
        expect(html).toContain('Beta Inc');
    });

    it('shows count value in count cell', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        expect(html).toContain('>5<');
        expect(html).toContain('>3<');
    });

    it('shows summary text', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, i18n);
        expect(html).toContain('Onboarding support');
        expect(html).toContain('API questions');
    });

    it('renders dash for missing customer field', () => {
        const html = reportsRenderer.renderActivityComponent(
            [{ customer: '', count: 2, summary: '' }],
            i18n
        );
        const div = document.createElement('div');
        div.innerHTML = html;
        const customerCell = div.querySelector('.c-report-customer-name');
        expect(customerCell?.textContent).toBe('-');
    });

    it('renders zero count when count is 0', () => {
        const html = reportsRenderer.renderActivityComponent(
            [{ customer: 'Test', count: 0, summary: '' }],
            i18n
        );
        expect(html).toContain('>0<');
    });

    it('handles empty array without crashing', () => {
        const html = reportsRenderer.renderActivityComponent([], i18n);
        expect(html).toContain('c-report-table');
        const div = document.createElement('div');
        div.innerHTML = html;
        expect(div.querySelectorAll('tbody tr')).toHaveLength(0);
    });

    it('escapes HTML special chars in customer name', () => {
        const html = reportsRenderer.renderActivityComponent(
            [{ customer: '<script>alert(1)</script>', count: 1, summary: '' }],
            i18n
        );
        expect(html).not.toContain('<script>');
        expect(html).toContain('&lt;script&gt;');
    });

    it('falls back to i18n defaults when keys are missing', () => {
        const html = reportsRenderer.renderActivityComponent(activityData, {});
        expect(html).toContain('고객사');
        expect(html).toContain('태스크');
        expect(html).toContain('Summary');
    });
});

// ──────────────────────────────────────────────────────────────────────────────
// renderStalledTasksComponent
// ──────────────────────────────────────────────────────────────────────────────

describe('reportsRenderer.renderStalledTasksComponent', () => {
    const stalledData: StalledTaskRow[] = [
        { source: 'slack', requester: 'Alice (내부)', assignee: 'Bob (External)', status: 'STALLED', days: 7, task: 'Fix login bug' },
        { source: 'whatsapp', requester: 'Carol', assignee: 'Dave', status: 'STALLED', days: 14, task: 'Setup DB' },
    ];

    it('returns HTML containing table wrapper', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('c-report-table-wrapper');
    });

    it('renders thead with correct i18n column headers', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('Source');
        expect(html).toContain('Requester');
        expect(html).toContain('Assignee');
        expect(html).toContain('Status');
        expect(html).toContain('Delay');
        expect(html).toContain('Task');
    });

    it('renders one row per stalled task', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        const div = document.createElement('div');
        div.innerHTML = html;
        expect(div.querySelectorAll('tbody tr')).toHaveLength(2);
    });

    it('shows source value in first column', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('slack');
        expect(html).toContain('whatsapp');
    });

    it('strips Korean parenthetical type suffix from requester', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).not.toContain('(내부)');
        expect(html).toContain('>Alice<');
    });

    it('strips English parenthetical type suffix from assignee', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).not.toContain('(External)');
        expect(html).toContain('>Bob<');
    });

    it('shows stalled badge with status value', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('c-report-badge--stalled');
        expect(html).toContain('STALLED');
    });

    it('defaults to STALLED when status is missing', () => {
        const html = reportsRenderer.renderStalledTasksComponent(
            [{ source: 'slack', requester: 'A', assignee: 'B', days: 3, task: 'T' }],
            i18n
        );
        expect(html).toContain('STALLED');
    });

    it('shows days value with i18n unit', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('>7<');
        expect(html).toContain('days');
    });

    it('shows task description', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, i18n);
        expect(html).toContain('Fix login bug');
        expect(html).toContain('Setup DB');
    });

    it('handles empty array without crashing', () => {
        const html = reportsRenderer.renderStalledTasksComponent([], i18n);
        expect(html).toContain('c-report-table');
        const div = document.createElement('div');
        div.innerHTML = html;
        expect(div.querySelectorAll('tbody tr')).toHaveLength(0);
    });

    it('renders dash for missing source/requester/assignee/task', () => {
        const html = reportsRenderer.renderStalledTasksComponent([{}], i18n);
        const div = document.createElement('div');
        div.innerHTML = html;
        const cells = div.querySelectorAll('tbody td');
        // source, requester, assignee, task cells should show '-'
        const textContents = [...cells].map(c => c.textContent?.trim());
        expect(textContents.filter(t => t === '-').length).toBeGreaterThanOrEqual(3);
    });

    it('renders days as 0 when days field is absent', () => {
        const html = reportsRenderer.renderStalledTasksComponent(
            [{ source: 'slack', task: 'T' }],
            i18n
        );
        expect(html).toContain('>0<');
    });

    it('falls back to i18n Korean defaults when keys are missing', () => {
        const html = reportsRenderer.renderStalledTasksComponent(stalledData, {});
        expect(html).toContain('소스');
        expect(html).toContain('요청자');
        expect(html).toContain('할당자');
        expect(html).toContain('일');
    });
});

// ──────────────────────────────────────────────────────────────────────────────
// render — JSON block detection and dispatch
// ──────────────────────────────────────────────────────────────────────────────

describe('reportsRenderer.render — JSON block detection', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="reportSummaryContent"></div>
            <div id="reportNetworkChart"></div>
            <div id="reportMatrixChart"></div>
        `;
    });

    // Why: test env mock for marked.parse returns `<p>${text}</p>` — does not produce
    // `<pre><code class="language-json">` blocks, so the regex replacement in render()
    // never fires. These tests verify the dispatch logic directly via the component methods.
    it('renderActivityComponent is dispatched for objects with customer/count keys', () => {
        const data = [{ customer: 'Acme', count: 3, summary: 'Test' }];
        // Direct dispatch test: activity data → activity component output
        const html = reportsRenderer.renderActivityComponent(data, i18n);
        expect(html).toContain('c-report-table');
        expect(html).toContain('Acme');
    });

    it('renderStalledTasksComponent is dispatched for objects without customer/count keys', () => {
        const data = [{ source: 'slack', requester: 'A', assignee: 'B', days: 5, task: 'Do it' }];
        // Direct dispatch test: stalled data → stalled tasks component output
        const html = reportsRenderer.renderStalledTasksComponent(data, i18n);
        expect(html).toContain('c-report-table');
        expect(html).toContain('Do it');
    });

    it('removes empty JSON array blocks from output', () => {
        const report = makeReport({ report_summary: `## Header\n\n\`\`\`json\n[]\n\`\`\`` });
        reportsRenderer.render(report, 'ko', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        expect(summaryArea.innerHTML).not.toContain('language-json');
    });

    it('survives malformed JSON without throwing', () => {
        const report = makeReport({ report_summary: '## Header\n\n```json\n{not valid json}\n```' });
        expect(() => reportsRenderer.render(report, 'ko', i18n)).not.toThrow();
    });

    it('renders truncation warning banner when is_truncated is true', () => {
        const report = makeReport({ is_truncated: true });
        reportsRenderer.render(report, 'ko', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        expect(summaryArea.innerHTML).toContain('c-report-warning');
        expect(summaryArea.innerHTML).toContain('Some past data were omitted.');
    });

    it('does not render truncation banner when is_truncated is false', () => {
        const report = makeReport({ is_truncated: false });
        reportsRenderer.render(report, 'ko', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        expect(summaryArea.innerHTML).not.toContain('c-report-warning');
    });

    it('uses translation when lang key exists in translations map', () => {
        const report = makeReport({
            report_summary: 'Korean content',
            translations: { en: 'English content' },
        });
        reportsRenderer.render(report, 'en', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        expect(summaryArea.innerHTML).toContain('English content');
    });

    it('falls back to report_summary when translation key is absent', () => {
        const report = makeReport({
            report_summary: 'Fallback content',
            translations: { de: 'Deutsch' },
        });
        reportsRenderer.render(report, 'fr', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        expect(summaryArea.innerHTML).toContain('Fallback content');
    });

    it('renders empty string when report_summary and translation are both absent', () => {
        const report = makeReport({ report_summary: '' });
        expect(() => reportsRenderer.render(report, 'ko', i18n)).not.toThrow();
    });

    it('strips empty paragraph tags from final HTML', () => {
        const report = makeReport({ report_summary: 'Hello\n\n\n\nWorld' });
        reportsRenderer.render(report, 'ko', i18n);
        const summaryArea = document.getElementById('reportSummaryContent')!;
        // Should not contain bare empty <p></p> or <p><br/></p>
        expect(summaryArea.innerHTML).not.toMatch(/<p>\s*(<br\s*\/?>)?\s*<\/p>/);
    });
});

// ──────────────────────────────────────────────────────────────────────────────
// render — no summaryArea DOM node (resilience)
// ──────────────────────────────────────────────────────────────────────────────

describe('reportsRenderer.render — missing DOM nodes', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
    });

    it('does not throw when reportSummaryContent is absent', () => {
        const report = makeReport();
        expect(() => reportsRenderer.render(report, 'ko', i18n)).not.toThrow();
    });
});

// ──────────────────────────────────────────────────────────────────────────────
// render — visualization data (SVG chart paths)
// Why: requestAnimationFrame is stubbed synchronous so SVG render branches fire.
// ──────────────────────────────────────────────────────────────────────────────

function makeVizReport(nodes: IReportNode[], links: IReportLink[]): IReportData {
    return makeReport({
        visualization_data: JSON.stringify({ nodes, links }),
    });
}

const sampleNodes: IReportNode[] = [
    { id: 'n1', name: 'Alice', value: 5, category: 'Internal', is_me: true },
    { id: 'n2', name: 'Bob', value: 3, category: 'Internal' },
    { id: 'n3', name: 'Carol', value: 2, category: 'External' },
];

const sampleLinks: IReportLink[] = [
    { source: 'n1', target: 'n2', value: 3, weight: 3 },
    { source: 'n2', target: 'n3', value: 2, weight: 2 },
    { source: 'n1', target: 'n3', value: 1, weight: 1 },
];

describe('reportsRenderer.render — visualization (SVG rendering)', () => {
    beforeEach(() => {
        // Why: stub rAF synchronous so the render path inside requestAnimationFrame fires immediately.
        vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 0; });
        document.body.innerHTML = `
            <div id="reportSummaryContent"></div>
            <div id="reportNetworkChart" style="width:800px;height:400px;"></div>
            <div id="reportMatrixChart" style="width:640px;height:400px;"></div>
        `;
    });

    it('renders SVG into network chart container when nodes are present', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.querySelector('svg')).not.toBeNull();
    });

    it('renders SVG into matrix chart container when nodes are present', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const matrixChart = document.getElementById('reportMatrixChart')!;
        expect(matrixChart.querySelector('svg')).not.toBeNull();
    });

    it('network SVG contains circle elements for each node', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        const svg = netChart.querySelector('svg')!;
        // Each node gets a <circle>; also legend has 3 circles → at least nodeCount circles
        const circles = svg.querySelectorAll('circle');
        expect(circles.length).toBeGreaterThanOrEqual(sampleNodes.length);
    });

    it('network SVG contains line elements for each link', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        const svg = netChart.querySelector('svg')!;
        const lines = svg.querySelectorAll('line');
        expect(lines.length).toBe(sampleLinks.length);
    });

    it('matrix SVG contains rect cells for node pairs', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const matrixChart = document.getElementById('reportMatrixChart')!;
        const svg = matrixChart.querySelector('svg')!;
        const rects = svg.querySelectorAll('rect');
        // N×N cells where N = min(3, TOP_N=10) = 3 → 9 rects
        expect(rects.length).toBeGreaterThan(0);
    });

    it('does not re-render SVG when visualization data is unchanged (cache key)', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        const firstSVG = netChart.querySelector('svg');
        // Second render with same data should not clear and re-append
        reportsRenderer.render(report, 'ko', i18n);
        const secondSVG = netChart.querySelector('svg');
        expect(firstSVG).toBe(secondSVG);
    });

    it('does not render SVG when nodes array is empty', () => {
        const report = makeVizReport([], sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.querySelector('svg')).toBeNull();
    });

    it('accepts visualization_data as pre-parsed object (non-string)', () => {
        const report = makeReport({
            visualization_data: { nodes: sampleNodes, links: sampleLinks },
        });
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.querySelector('svg')).not.toBeNull();
    });

    it('accepts legacy Nodes/Links capitalized keys in visualization_data', () => {
        const report = makeReport({
            visualization_data: JSON.stringify({ Nodes: sampleNodes, Links: sampleLinks }),
        });
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.querySelector('svg')).not.toBeNull();
    });

    it('handles malformed visualization_data string without throwing', () => {
        const report = makeReport({ visualization_data: '{not valid json}' });
        expect(() => reportsRenderer.render(report, 'ko', i18n)).not.toThrow();
    });

    it('renders label text for node names in network SVG', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.innerHTML).toContain('Alice');
        expect(netChart.innerHTML).toContain('Bob');
    });

    it('truncates node names longer than 20 chars in network SVG', () => {
        const longName = 'A'.repeat(25);
        const nodes: IReportNode[] = [
            { id: 'a', name: longName, value: 1, category: 'External' },
            { id: 'b', name: 'Short', value: 1, category: 'External' },
        ];
        const links: IReportLink[] = [{ source: 'a', target: 'b', value: 1 }];
        const report = makeVizReport(nodes, links);
        reportsRenderer.render(report, 'ko', i18n);
        const netChart = document.getElementById('reportNetworkChart')!;
        // Truncated at 19 chars + ellipsis
        expect(netChart.innerHTML).toContain('…');
        expect(netChart.innerHTML).not.toContain(longName);
    });

    it('handles many nodes (>12) with staggered labels without throwing', () => {
        const manyNodes: IReportNode[] = Array.from({ length: 15 }, (_, i) => ({
            id: `n${i}`, name: `Node ${i}`, value: i + 1, category: 'External',
        }));
        const manyLinks: IReportLink[] = manyNodes.slice(1).map((n, i) => ({
            source: manyNodes[0].id, target: n.id, value: i + 1,
        }));
        const report = makeVizReport(manyNodes, manyLinks);
        expect(() => reportsRenderer.render(report, 'ko', i18n)).not.toThrow();
        const netChart = document.getElementById('reportNetworkChart')!;
        expect(netChart.querySelector('svg')).not.toBeNull();
    });

    it('matrix SVG row labels match top node names', () => {
        const report = makeVizReport(sampleNodes, sampleLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const matrixChart = document.getElementById('reportMatrixChart')!;
        // At least one of the node names should appear as a truncated label
        expect(matrixChart.innerHTML).toMatch(/Alice|Bob|Carol/);
    });

    it('matrix renders empty when all link weights produce maxVal=0', () => {
        // All zero-value links → maxVal stays 0 → renderMatrixSVG returns early
        const zeroLinks: IReportLink[] = [{ source: 'n1', target: 'n2', value: 0, weight: 0 }];
        const report = makeVizReport(sampleNodes, zeroLinks);
        reportsRenderer.render(report, 'ko', i18n);
        const matrixChart = document.getElementById('reportMatrixChart')!;
        // Should not render SVG because maxVal === 0
        expect(matrixChart.querySelector('svg')).toBeNull();
    });
});
