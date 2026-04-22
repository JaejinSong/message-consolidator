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

const NODE_COLORS = {
    me:       'var(--color-warning)',
    internal: 'var(--color-success)',
    external: 'var(--color-primary)',
};

function getNodeColor(n: any): string {
    if (n.is_me) return NODE_COLORS.me;
    if (n.category === 'Internal') return NODE_COLORS.internal;
    return NODE_COLORS.external;
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
    const radius = Math.min(width, height) / 2 - 90;
    const coords = new Map<string, { x: number; y: number; angle: number }>();
    const angleStep = (2 * Math.PI) / (nodes.length || 1);

    nodes.forEach((node, i) => {
        const angle = i * angleStep - Math.PI / 2;
        coords.set(node.id, { x: centerX + radius * Math.cos(angle), y: centerY + radius * Math.sin(angle), angle });
        if (node.value === undefined) {
            node.value = links.reduce((sum, l) => sum + (l.source === node.id || l.target === node.id ? (l.weight || 1) : 0), 0);
        }
    });

    links.forEach(l => {
        const s = coords.get(l.source);
        const t = coords.get(l.target);
        if (!s || !t) return;
        const sw = Math.max(1, Math.min(6, Math.sqrt(l.weight || 1) * 1.5));
        const line = createSVGElement('line', {
            x1: s.x, y1: s.y, x2: t.x, y2: t.y,
            'stroke-width': sw, stroke: 'var(--text-dim)', opacity: '0.2'
        });
        line.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(getNodeName(nodes, l.source))} ↔ ${escapeHTML(getNodeName(nodes, l.target))}</b><br/>Connections: ${l.weight || 1}`));
        line.addEventListener('mouseenter', () => { line.setAttribute('opacity', '0.9'); line.setAttribute('stroke', NODE_COLORS.me); });
        line.addEventListener('mouseleave', () => { hideTooltip(); line.setAttribute('opacity', '0.2'); line.setAttribute('stroke', 'var(--text-dim)'); });
        svg.appendChild(line);
    });

    nodes.forEach(n => {
        const p = coords.get(n.id);
        if (!p) return;
        const r = Math.max(5, 3 + Math.sqrt(n.value || 1) * 2.2);
        const color = getNodeColor(n);
        const g = createSVGElement('g', { style: 'cursor:pointer' });

        const circle = createSVGElement('circle', {
            cx: p.x, cy: p.y, r, fill: color,
            stroke: n.is_me ? 'var(--text-main)' : 'none', 'stroke-width': n.is_me ? '2' : '0'
        });
        g.appendChild(circle);

        // Label radiates outward from center
        const labelDist = r + 8;
        const lx = p.x + Math.cos(p.angle) * labelDist;
        const ly = p.y + Math.sin(p.angle) * labelDist;
        const anchor = Math.cos(p.angle) > 0.15 ? 'start' : Math.cos(p.angle) < -0.15 ? 'end' : 'middle';
        const raw = n.name || n.id;
        const label = raw.length > 18 ? raw.slice(0, 17) + '…' : raw;

        const text = createSVGElement('text', {
            x: lx, y: ly, 'text-anchor': anchor, 'dominant-baseline': 'middle',
            fill: 'var(--text-main)',
            style: `font-size:0.65rem;font-weight:${n.is_me ? '700' : '400'};paint-order:stroke;stroke:var(--bg-color);stroke-width:0.2rem;stroke-linejoin:round;`
        });
        text.textContent = label;
        g.appendChild(text);

        g.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(raw)}</b><br/>Activity: ${n.value}<br/>${escapeHTML(n.category || 'External')}`));
        g.addEventListener('mouseenter', () => { circle.setAttribute('stroke', 'var(--text-main)'); circle.setAttribute('stroke-width', '2'); });
        g.addEventListener('mouseleave', () => { hideTooltip(); circle.setAttribute('stroke', n.is_me ? 'var(--text-main)' : 'none'); circle.setAttribute('stroke-width', n.is_me ? '2' : '0'); });
        svg.appendChild(g);
    });

    // Legend
    ([['me', 'Me'], ['internal', 'Internal'], ['external', 'External']] as const).forEach(([key, label], i) => {
        const lx = 16, ly = 18 + i * 20;
        svg.appendChild(createSVGElement('circle', { cx: lx, cy: ly, r: 5, fill: NODE_COLORS[key] }));
        const t = createSVGElement('text', { x: lx + 12, y: ly, 'dominant-baseline': 'middle', fill: 'var(--text-dim)', style: 'font-size:0.68rem;' });
        t.textContent = label;
        svg.appendChild(t);
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
