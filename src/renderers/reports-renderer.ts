import { IReportData, ParsedVisualization } from '../types';
import { parseMarkdown } from '../logic';

/**
 * Reports Vanilla SVG Renderer
 * Handles rendering of Network Graphs and Sankey Charts using pure SVG.
 */

const SVG_NS = 'http://www.w3.org/2000/svg';

/**
 * Creates an SVG element with attributes.
 */
function createSVGElement(tag: string, attrs: Record<string, string | number>): SVGElement {
    const el = document.createElementNS(SVG_NS, tag);
    Object.entries(attrs).forEach(([key, val]) => el.setAttribute(key, String(val)));
    return el;
}

/**
 * Calculates coordinates for a circular layout.
 */
function calculateCircularLayout(nodes: any[], centerX: number, centerY: number, radius: number) {
    const coords = new Map<string, { x: number, y: number }>();
    const angleStep = (2 * Math.PI) / (nodes.length || 1);

    nodes.forEach((node, i) => {
        const angle = i * angleStep;
        coords.set(node.id, {
            x: centerX + radius * Math.cos(angle),
            y: centerY + radius * Math.sin(angle)
        });
    });
    return coords;
}

/**
 * Renders a Network Graph using SVG.
 */
function renderNetworkSVG(container: HTMLElement, nodes: any[], links: any[]): void {
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });
    const coords = calculateCircularLayout(nodes, width / 2, height / 2, Math.min(width, height) / 2 - 40);

    links.forEach(l => {
        const s = coords.get(l.source);
        const t = coords.get(l.target);
        if (s && t) {
            svg.appendChild(createSVGElement('line', {
                x1: s.x, y1: s.y, x2: t.x, y2: t.y,
                class: 'c-report-viz__link',
                'stroke-width': Math.sqrt(l.value || 1)
            }));
        }
    });

    nodes.forEach(n => {
        const p = coords.get(n.id);
        if (p) {
            svg.appendChild(createSVGElement('circle', {
                cx: p.x, cy: p.y, r: 6,
                class: `c-report-viz__node ${n.is_me ? 'c-report-viz__node--me' : ''}`
            }));
        }
    });
    container.appendChild(svg);
}

/**
 * Renders a Sankey Chart using SVG Cubic Bezier paths.
 */
function renderSankeySVG(container: HTMLElement, nodes: any[], links: any[]): void {
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });
    
    const sources = nodes.filter(n => links.some(l => l.source === n.id));
    const targets = nodes.filter(n => links.some(l => l.target === n.id && !sources.includes(n)));

    const sCoords = calculateColumnLayout(sources, 80, height);
    const tCoords = calculateColumnLayout(targets, width - 80, height);

    links.forEach(l => {
        const s = sCoords.get(l.source);
        const t = tCoords.get(l.target);
        if (!s || !t) return;
        const d = `M ${s.x} ${s.y} C ${(s.x + t.x) / 2} ${s.y}, ${(s.x + t.x) / 2} ${t.y}, ${t.x} ${t.y}`;
        svg.appendChild(createSVGElement('path', { d, class: 'c-report-viz__link', fill: 'none', 'stroke-width': Math.max(2, l.value / 2) }));
    });

    [...sCoords.values(), ...tCoords.values()].forEach(p => {
        svg.appendChild(createSVGElement('rect', { x: p.x - 4, y: p.y - 15, width: 8, height: 30, class: 'c-report-viz__node', rx: 2 }));
    });
    container.appendChild(svg);
}

function calculateColumnLayout(nodes: any[], x: number, height: number) {
    const coords = new Map<string, { x: number, y: number }>();
    const step = height / (nodes.length + 1);
    nodes.forEach((n, i) => coords.set(n.id, { x, y: (i + 1) * step }));
    return coords;
}

export const reportsRenderer = {
    renderHistory(container: HTMLElement, items: IReportData[], onSelect: (item: IReportData) => void, i18n: any): void {
        container.innerHTML = '';
        if (!items || items.length === 0) {
            container.innerHTML = `<div class="u-text-dim u-p-4">${i18n.noReports || 'No reports found.'}</div>`;
            return;
        }

        items.forEach(item => {
            const btn = document.createElement('div');
            btn.className = 'c-insights-report-item';
            btn.setAttribute('data-id', String(item.id));
            btn.innerHTML = `
                <div class="c-insights-report-item__title u-text-xs u-text-dim">${item.title || (i18n.weeklyReportTitle || 'Weekly Report')}</div>
                <div class="c-insights-report-item__meta">${item.start_date} ~ ${item.end_date}</div>
            `;
            btn.onclick = () => {
                container.querySelectorAll('.c-insights-report-item').forEach(el => el.classList.remove('c-insights-report-item--active'));
                btn.classList.add('c-insights-report-item--active');
                onSelect(item);
            };
            container.appendChild(btn);
        });

    },

    render(report: IReportData, lang: string, i18n: any): void {
        const summaryArea = document.getElementById('reportSummaryContent');
        const netChartArea = document.getElementById('reportNetworkChart');
        const sankeyChartArea = document.getElementById('reportSankeyChart');

        // Field mapping: support translations first, then 'summary', then legacy 'report_summary'
        const summaryText = report.translations?.[lang] || report.summary || report.report_summary || "";
        
        if (summaryArea) summaryArea.innerHTML = parseMarkdown(summaryText);
        
        // Headers/Labels
        const summaryTitle = document.querySelector('.c-report-summary-title');
        if (summaryTitle) summaryTitle.textContent = i18n.reportSummaryTitle || '주간 업무 요약';
        const vizTitle = document.querySelector('.c-report-viz-title');
        if (vizTitle) vizTitle.textContent = i18n.reportVizTitle || '커뮤니케이션 관계망';
        const flowTitle = document.querySelector('.c-report-flow-title');
        if (flowTitle) flowTitle.textContent = i18n.reportSankeyTitle || '커뮤니케이션 흐름 (Sankey)';

        // Field mapping: support both 'visualization' and legacy 'visualization_data'
        const vizRaw = report.visualization || report.visualization_data;
        let viz: ParsedVisualization;
        
        try {
            if (typeof vizRaw === 'string' && vizRaw.trim()) {
                viz = JSON.parse(vizRaw);
            } else if (vizRaw && typeof vizRaw === 'object') {
                viz = vizRaw as ParsedVisualization;
            } else {
                viz = { nodes: [], links: [] };
            }
        } catch (e) {
            console.error("[ReportsRenderer] Visualization data parsing failed:", e);
            viz = { nodes: [], links: [] };
        }

        if (netChartArea) {
            netChartArea.innerHTML = '';
            renderNetworkSVG(netChartArea, viz.nodes || [], viz.links || []);
        }
        if (sankeyChartArea) {
            sankeyChartArea.innerHTML = '';
            renderSankeySVG(sankeyChartArea, viz.nodes || [], viz.links || []);
        }
    }
};
