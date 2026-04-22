import { parseMarkdown } from '../logic';
import { IReportData, ParsedVisualization } from '../types';
import { escapeHTML } from '../utils';

/**
 * @file reports-renderer.ts
 * @description Handles the rendering of complex AI-generated reports including JSON-driven tables and SVG charts.
 */

const SVG_NS = 'http://www.w3.org/2000/svg';

/**
 * Creates an SVG element with attributes.
 */
function createSVGElement(tag: string, attrs: Record<string, string | number>): SVGElement {
    const el = document.createElementNS(SVG_NS, tag);
    Object.entries(attrs).forEach(([key, val]) => {
        if (val !== undefined && val !== null) {
            el.setAttribute(key, String(val));
        }
    });
    return el;
}

/**
 * Tooltip Helpers
 */
function showTooltip(e: MouseEvent, content: string) {
    let tooltip = document.getElementById('report-tooltip');
    if (!tooltip) {
        tooltip = document.createElement('div');
        tooltip.id = 'report-tooltip';
        tooltip.className = 'c-insights-tooltip';
        tooltip.style.position = 'absolute';
        tooltip.style.pointerEvents = 'none';
        tooltip.style.zIndex = 'var(--z-index-modal)';
        tooltip.style.background = 'var(--bg-floater)';
        tooltip.style.color = 'var(--text-main)';
        tooltip.style.padding = 'var(--spacing-sm) var(--spacing-md)';
        tooltip.style.borderRadius = 'var(--radius-sm)';
        tooltip.style.boxShadow = 'var(--shadow-card)';
        tooltip.style.fontSize = '0.75rem';
        tooltip.style.whiteSpace = 'nowrap';
        document.body.appendChild(tooltip);
    }
    tooltip.innerHTML = content;
    tooltip.style.left = `${e.pageX + 15}px`;
    tooltip.style.top = `${e.pageY + 15}px`;
    tooltip.style.display = 'block';
}

function hideTooltip() {
    const tooltip = document.getElementById('report-tooltip');
    if (tooltip) {
        tooltip.style.display = 'none';
    }
}

function getNodeName(nodes: any[], id: string) {
    const n = nodes.find(n => n.id === id);
    return n ? (n.name || n.id) : id;
}

/**
 * Renders a Network Graph using SVG.
 */
function renderNetworkSVG(container: HTMLElement, nodes: any[], links: any[]): void {
    if (!container || !Array.isArray(nodes) || !Array.isArray(links)) return;
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });
    
    const centerX = width / 2;
    const centerY = height / 2;
    const radius = Math.min(width, height) / 2 - 60;
    const coords = new Map<string, { x: number, y: number }>();
    const angleStep = (2 * Math.PI) / (nodes.length || 1);

    nodes.forEach((node, i) => {
        const angle = i * angleStep;
        coords.set(node.id, {
            x: centerX + radius * Math.cos(angle),
            y: centerY + radius * Math.sin(angle)
        });
        if (node.value === undefined) {
            node.value = links.reduce((sum, l) => sum + (l.source === node.id || l.target === node.id ? (l.value || 1) : 0), 0);
        }
    });

    const nodeShapes = new Map<string, SVGElement>();

    links.forEach(l => {
        const s = coords.get(l.source);
        const t = coords.get(l.target);
        if (s && t) {
            const sw = Math.max(1, Math.min(8, Math.sqrt(l.weight || 1) * 2));
            const line = createSVGElement('line', {
                x1: s.x, y1: s.y, x2: t.x, y2: t.y,
                class: 'c-report-viz__link',
                'stroke-width': sw,
                stroke: 'var(--color-primary)',
                opacity: '0.4'
            });
            line.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(getNodeName(nodes, l.source))} ↔ ${escapeHTML(getNodeName(nodes, l.target))}</b><br/>Connections: ${l.weight || 1}`));
            line.addEventListener('mouseenter', () => {
                line.setAttribute('opacity', '1');
                line.setAttribute('stroke', 'var(--color-warning)');
            });
            line.addEventListener('mouseleave', () => {
                hideTooltip();
                line.setAttribute('opacity', '0.4');
                line.setAttribute('stroke', 'var(--color-primary)');
            });
            svg.appendChild(line);
        }
    });

    nodes.forEach(n => {
        const p = coords.get(n.id);
        if (p) {
            const r = Math.max(6, 4 + Math.sqrt(n.value || 1) * 3);
            const g = createSVGElement('g', { class: 'c-report-viz__node-group' });
            const nodeColor = n.is_me ? 'var(--color-primary)' : n.category === 'Internal' ? 'var(--color-success)' : 'var(--color-info)';
            const circle = createSVGElement('circle', {
                cx: p.x, cy: p.y, r,
                class: `c-report-viz__node ${n.is_me ? 'c-report-viz__node--me' : ''}`,
                fill: nodeColor
            });
            nodeShapes.set(n.id, circle);
            g.appendChild(circle);

            const text = createSVGElement('text', {
                x: p.x, y: p.y + r + 14,
                'text-anchor': 'middle',
                fill: 'var(--text-main)',
                style: `font-size: 0.75rem; font-weight: ${n.is_me ? 'bold' : 'normal'}`
            });
            text.textContent = n.name || n.id;
            g.appendChild(text);

            g.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(n.name || n.id)}</b><br/>Activity: ${n.value}<br/>${escapeHTML(n.category || '')}`));
            g.addEventListener('mouseenter', () => { circle.setAttribute('stroke', 'var(--color-warning)'); circle.setAttribute('stroke-width', '3'); });
            g.addEventListener('mouseleave', () => { hideTooltip(); circle.removeAttribute('stroke'); circle.removeAttribute('stroke-width'); });
            svg.appendChild(g);
        }
    });
    container.appendChild(svg);
}

/**
 * Renders a Sankey Chart using SVG.
 */
function renderSankeySVG(container: HTMLElement, nodes: any[], links: any[]): void {
    if (!container || !Array.isArray(nodes) || !Array.isArray(links)) return;
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });

    nodes.forEach(n => {
        if (n.value === undefined) {
            n.value = links.reduce((sum, l) => sum + (l.source === n.id || l.target === n.id ? (l.weight || 1) : 0), 0);
        }
    });

    const sources = nodes.filter(n => links.some(l => l.source === n.id));
    const targets = nodes.filter(n => links.some(l => l.target === n.id && !sources.includes(n)));

    const createLayout = (list: any[], x: number) => {
        const coords = new Map<string, { x: number, y: number }>();
        const step = height / (list.length + 1);
        list.forEach((n, i) => coords.set(n.id, { x, y: (i + 1) * step }));
        return coords;
    };

    const sCoords = createLayout(sources, 120);
    const tCoords = createLayout(targets, width - 120);

    links.forEach(l => {
        const s = sCoords.get(l.source);
        const t = tCoords.get(l.target);
        if (!s || !t) return;
        const sw = Math.max(2, Math.min(16, Math.sqrt(l.weight || 1) * 3));
        const d = `M ${s.x} ${s.y} C ${(s.x + t.x) / 2} ${s.y}, ${(s.x + t.x) / 2} ${t.y}, ${t.x} ${t.y}`;
        const path = createSVGElement('path', {
            d, class: 'c-report-viz__link', fill: 'none', 'stroke-width': sw,
            stroke: 'var(--color-primary)', opacity: '0.4'
        });
        path.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(getNodeName(nodes, l.source))} → ${escapeHTML(getNodeName(nodes, l.target))}</b><br/>Connections: ${l.weight || 1}`));
        path.addEventListener('mouseenter', () => { path.setAttribute('opacity', '1'); path.setAttribute('stroke', 'var(--color-warning)'); });
        path.addEventListener('mouseleave', () => { hideTooltip(); path.setAttribute('opacity', '0.4'); path.setAttribute('stroke', 'var(--color-primary)'); });
        svg.appendChild(path);
    });

    [...sCoords, ...tCoords].forEach(([id, p]) => {
        const n = nodes.find(node => node.id === id);
        if (!n) return;
        const h = Math.max(16, 10 + Math.sqrt(n.value || 1) * 4);
        const rectColor = n.is_me ? 'var(--color-primary)' : n.category === 'Internal' ? 'var(--color-success)' : 'var(--color-info)';
        const rect = createSVGElement('rect', {
            x: p.x - 4, y: p.y - h / 2, width: 8, height: h,
            class: 'c-report-viz__node', rx: 2,
            fill: rectColor
        });
        svg.appendChild(rect);

        const isSource = sCoords.has(id);
        const text = createSVGElement('text', {
            x: p.x + (isSource ? -12 : 12),
            y: p.y + 4,
            'text-anchor': isSource ? 'end' : 'start',
            fill: 'var(--text-main)',
            style: 'font-size: 0.7rem; font-weight: 500;'
        });
        text.textContent = n.name || n.id;
        svg.appendChild(text);
    });

    container.appendChild(svg);
}

export const reportsRenderer = {
    renderHistory(container: HTMLElement, items: IReportData[], onSelect: (item: IReportData) => void, i18n: any): void {
        container.innerHTML = '';
        if (!items || items.length === 0) {
            container.innerHTML = `<div class="u-text-dim u-p-4">${i18n.noReports || 'Reports not found.'}</div>`;
            return;
        }

        items.forEach(item => {
            const btn = document.createElement('div');
            btn.className = 'c-insights-report-item';
            btn.setAttribute('data-id', String(item.id));
            
            let statusTag = '';
            if (item.status === 'processing') statusTag = `⌛`;
            else if (item.status === 'failed') statusTag = `⚠️`;
            else if (item.status === 'completed') statusTag = `✅`;

            btn.innerHTML = `
                <div class="c-insights-report-item__content">
                    <span class="c-insights-report-item__date">${item.start_date} ~ ${item.end_date}</span>
                    <div class="c-insights-report-item__title">${statusTag} ${item.title || 'Weekly Report'}</div>
                </div>
                <button class="c-insights-report-item__delete" data-id="${item.id}" title="${i18n.delete || 'Delete'}">
                    <i class="fas fa-trash-alt"></i>
                </button>
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

        const summaryText = report.translations?.[lang] || report.report_summary || "";

        if (summaryArea) {
            const html = parseMarkdown(summaryText);
            const stalledMatch = summaryText.match(/## \[Stalled Tasks\]\s*```json\s*([\s\S]*?)```/);
            
            if (stalledMatch) {
                try {
                    const stalledData = JSON.parse(stalledMatch[1]);
                    const renderedTable = this.renderStalledTasksComponent(stalledData, i18n);
                    summaryArea.innerHTML = html.replace(/<pre><code class="language-json">[\s\S]*?<\/code><\/pre>/, renderedTable);
                } catch (e) {
                    console.error("[ReportsRenderer] JSON parse failed:", e);
                    summaryArea.innerHTML = html;
                }
            } else {
                summaryArea.innerHTML = html;
            }
        }

        const vizRaw = report.visualization_data;
        let viz: ParsedVisualization = { nodes: [], links: [] };

        try {
            if (vizRaw) {
                const parsed = typeof vizRaw === 'string' ? JSON.parse(vizRaw) : vizRaw;
                viz = { nodes: parsed.nodes || parsed.Nodes || [], links: parsed.links || parsed.Links || [] };
            }
        } catch (e) {
            console.error("[ReportsRenderer] Viz parse failed:", e);
        }

        if (viz.nodes.length > 0) {
            const vizKey = JSON.stringify(viz);
            if (netChartArea && netChartArea.dataset.vizKey !== vizKey) {
                netChartArea.dataset.vizKey = vizKey;
                requestAnimationFrame(() => { netChartArea.innerHTML = ''; renderNetworkSVG(netChartArea, viz.nodes, viz.links); });
            }
            if (sankeyChartArea && sankeyChartArea.dataset.vizKey !== vizKey) {
                sankeyChartArea.dataset.vizKey = vizKey;
                requestAnimationFrame(() => { sankeyChartArea.innerHTML = ''; renderSankeySVG(sankeyChartArea, viz.nodes, viz.links); });
            }
        }
    },

    renderStalledTasksComponent(data: any[], i18n: any): string {
        const rows = data.map(item => `
            <tr>
                <td class="c-report-table__cell--source">${item.source || '-'}</td>
                <td><span class="u-font-bold">${item.requester || '-'}</span></td>
                <td><span class="c-report-badge c-report-badge--stalled">${item.status || 'Stalled'}</span></td>
                <td class="c-report-table__cell--days"><span class="c-report-delay-value">${item.days || 0}</span> ${i18n.days || '일'}</td>
                <td>${item.reason || '-'}</td>
            </tr>
        `).join('');

        return `
            <div class="c-report-table-wrapper u-mt-4">
                <table class="c-report-table">
                    <thead>
                        <tr>
                            <th>${i18n.source || '소스'}</th>
                            <th>${i18n.requester || '요청자'}</th>
                            <th>${i18n.status || '상태'}</th>
                            <th>${i18n.delay || '지연'}</th>
                            <th>${i18n.rootCause || '원인'}</th>
                        </tr>
                    </thead>
                    <tbody>${rows}</tbody>
                </table>
            </div>
        `;
    }
};
