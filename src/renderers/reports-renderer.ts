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
    const nodeCount = nodes.length;
    // Larger radius gives more arc space between adjacent nodes for labels
    const radius = Math.min(width, height) / 2 - (nodeCount > 18 ? 65 : 55);
    const coords = new Map<string, { x: number; y: number; angle: number; stagger: number }>();
    const angleStep = (2 * Math.PI) / (nodeCount || 1);
    // Two stagger levels when dense: even-indexed labels push out 16px further
    const useStagger = nodeCount > 12;

    nodes.forEach((node, i) => {
        const angle = i * angleStep - Math.PI / 2;
        const stagger = useStagger ? (i % 2) * 16 : 0;
        coords.set(node.id, { x: centerX + radius * Math.cos(angle), y: centerY + radius * Math.sin(angle), angle, stagger });
        if (node.value === undefined) {
            node.value = links.reduce((sum, l) => sum + (l.source === node.id || l.target === node.id ? (l.weight || 1) : 0), 0);
        }
    });

    // Index for hover highlight
    const lineEls: Array<{ el: SVGElement; source: string; target: string }> = [];
    const nodeEls = new Map<string, { g: SVGElement; circle: SVGElement }>();

    const highlightNode = (nodeId: string) => {
        const connected = new Set<string>([nodeId]);
        lineEls.forEach(({ el, source, target }) => {
            const hit = source === nodeId || target === nodeId;
            el.setAttribute('opacity', hit ? '0.9' : '0.04');
            el.setAttribute('stroke', hit ? NODE_COLORS.me : 'var(--text-dim)');
            if (hit) { connected.add(source); connected.add(target); }
        });
        nodeEls.forEach(({ g }, id) => g.setAttribute('opacity', connected.has(id) ? '1' : '0.2'));
    };

    const resetHighlight = () => {
        lineEls.forEach(({ el }) => { el.setAttribute('opacity', '0.2'); el.setAttribute('stroke', 'var(--text-dim)'); });
        nodeEls.forEach(({ g }) => g.setAttribute('opacity', '1'));
    };

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
        lineEls.push({ el: line, source: l.source, target: l.target });
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

        // Label radiates outward, staggered to reduce overlap in dense layouts
        const labelDist = r + 10 + p.stagger;
        const lx = p.x + Math.cos(p.angle) * labelDist;
        const ly = p.y + Math.sin(p.angle) * labelDist;
        const anchor = Math.cos(p.angle) > 0.15 ? 'start' : Math.cos(p.angle) < -0.15 ? 'end' : 'middle';
        const raw = n.name || n.id;
        const label = raw.length > 20 ? raw.slice(0, 19) + '…' : raw;

        const text = createSVGElement('text', {
            x: lx, y: ly, 'text-anchor': anchor, 'dominant-baseline': 'middle',
            fill: 'var(--text-main)',
            style: `font-size:0.75rem;font-weight:${n.is_me ? '700' : '400'};paint-order:stroke;stroke:var(--bg-color);stroke-width:0.25rem;stroke-linejoin:round;`
        });
        text.textContent = label;
        g.appendChild(text);

        nodeEls.set(n.id, { g, circle });

        g.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, `<b>${escapeHTML(raw)}</b><br/>Activity: ${n.value}<br/>${escapeHTML(n.category || 'External')}`));
        g.addEventListener('mouseenter', () => {
            circle.setAttribute('stroke', 'var(--text-main)');
            circle.setAttribute('stroke-width', '2');
            highlightNode(n.id);
        });
        g.addEventListener('mouseleave', () => {
            hideTooltip();
            circle.setAttribute('stroke', n.is_me ? 'var(--text-main)' : 'none');
            circle.setAttribute('stroke-width', n.is_me ? '2' : '0');
            resetHighlight();
        });
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

const SANKEY_PALETTE = [
    'hsl(210,80%,65%)', 'hsl(150,65%,55%)', 'hsl(45,90%,60%)',
    'hsl(280,65%,68%)', 'hsl(0,72%,65%)',   'hsl(185,70%,55%)',
    'hsl(330,68%,65%)', 'hsl(30,80%,62%)',  'hsl(240,60%,72%)',
    'hsl(90,62%,58%)',
];

/**
 * Renders a Sankey Chart using SVG.
 * Each source gets a distinct color; hover highlights all paths from that source.
 */
function renderSankeySVG(container: HTMLElement, nodes: any[], links: any[]): void {
    if (!container || !Array.isArray(nodes) || !Array.isArray(links)) return;
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });

    const outVol = new Map<string, number>();
    const inVol  = new Map<string, number>();
    links.forEach(l => {
        const v = l.value ?? l.weight ?? 1;
        outVol.set(l.source, (outVol.get(l.source) || 0) + v);
        inVol.set(l.target,  (inVol.get(l.target)  || 0) + v);
    });

    const sourceIds = [...new Set(links.map(l => l.source))]
        .sort((a, b) => (outVol.get(b) || 0) - (outVol.get(a) || 0));
    const targetIds = [...new Set(links.map(l => l.target))]
        .filter(id => !sourceIds.includes(id))
        .sort((a, b) => (inVol.get(b) || 0) - (inVol.get(a) || 0));

    const sourceColor = new Map(sourceIds.map((id, i) => [id, SANKEY_PALETTE[i % SANKEY_PALETTE.length]]));

    const layout = (ids: string[], x: number) => {
        const step = height / (ids.length + 1);
        return new Map(ids.map((id, i) => [id, { x, y: (i + 1) * step }]));
    };
    const sCoords = layout(sourceIds, 130);
    const tCoords = layout(targetIds, width - 130);

    const pathsBySource = new Map<string, SVGElement[]>();

    links.forEach(l => {
        const s = sCoords.get(l.source);
        const t = tCoords.get(l.target);
        if (!s || !t) return;
        const v   = l.value ?? l.weight ?? 1;
        const sw  = Math.max(2, Math.min(20, Math.sqrt(v) * 4));
        const col = sourceColor.get(l.source) || 'var(--color-primary)';
        const mx  = (s.x + t.x) / 2;
        const d   = `M ${s.x} ${s.y} C ${mx} ${s.y}, ${mx} ${t.y}, ${t.x} ${t.y}`;

        const path = createSVGElement('path', {
            d, fill: 'none', 'stroke-width': sw, stroke: col, opacity: '0.45'
        });
        const tip = `<b>${escapeHTML(getNodeName(nodes, l.source))} → ${escapeHTML(getNodeName(nodes, l.target))}</b><br/>${v}건`;
        path.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, tip));
        path.addEventListener('mouseenter', () => pathsBySource.get(l.source)?.forEach(p => p.setAttribute('opacity', '1')));
        path.addEventListener('mouseleave', () => { hideTooltip(); pathsBySource.get(l.source)?.forEach(p => p.setAttribute('opacity', '0.45')); });

        if (!pathsBySource.has(l.source)) pathsBySource.set(l.source, []);
        pathsBySource.get(l.source)!.push(path);
        svg.appendChild(path);
    });

    const NODE_W = 12;
    [...sCoords, ...tCoords].forEach(([id, p]) => {
        const n = nodes.find(node => node.id === id);
        if (!n) return;
        const isSource = sCoords.has(id);
        const totalVol = (outVol.get(id) || 0) + (inVol.get(id) || 0);
        const h   = Math.max(20, 8 + Math.sqrt(totalVol) * 5);
        const col = isSource
            ? (sourceColor.get(id) || 'var(--color-primary)')
            : (n.is_me ? 'var(--color-warning)' : n.category === 'Internal' ? 'var(--color-success)' : 'var(--color-info)');

        const rect = createSVGElement('rect', {
            x: p.x - NODE_W / 2, y: p.y - h / 2, width: NODE_W, height: h, rx: 3, fill: col
        });
        rect.addEventListener('mouseenter', () => pathsBySource.get(id)?.forEach(p => p.setAttribute('opacity', '1')));
        rect.addEventListener('mouseleave', () => pathsBySource.get(id)?.forEach(p => p.setAttribute('opacity', '0.45')));
        svg.appendChild(rect);

        const raw = n.name || n.id;
        const label = raw.length > 18 ? raw.slice(0, 17) + '…' : raw;
        const text = createSVGElement('text', {
            x: p.x + (isSource ? -(NODE_W / 2 + 6) : (NODE_W / 2 + 6)),
            y: p.y, 'text-anchor': isSource ? 'end' : 'start', 'dominant-baseline': 'middle',
            fill: 'var(--text-main)', style: 'font-size:0.72rem;font-weight:500;'
        });
        text.textContent = label;
        svg.appendChild(text);
    });

    container.appendChild(svg);
}

const TOP_N = 10;
const MATRIX_LABEL_W = 130;
const MATRIX_LABEL_H = 90;
const MATRIX_TITLE_H = 28;

function buildMatrixData(nodes: any[], links: any[], topN: number) {
    const vol = new Map<string, number>();
    links.forEach(l => {
        const v = l.value ?? l.weight ?? 1;
        vol.set(l.source, (vol.get(l.source) || 0) + v);
        vol.set(l.target, (vol.get(l.target) || 0) + v);
    });
    const allIds = [...new Set<string>(links.flatMap(l => [l.source, l.target]))];
    const topIds = allIds.sort((a, b) => (vol.get(b) || 0) - (vol.get(a) || 0)).slice(0, topN);
    const topSet = new Set(topIds);
    const matrix = new Map<string, Map<string, number>>();
    topIds.forEach(src => matrix.set(src, new Map(topIds.map(tgt => [tgt, 0]))));
    links.forEach(l => {
        if (!topSet.has(l.source) || !topSet.has(l.target) || l.source === l.target) return;
        const row = matrix.get(l.source)!;
        row.set(l.target, (row.get(l.target) || 0) + (l.value ?? l.weight ?? 1));
    });
    let maxVal = 0;
    matrix.forEach(row => row.forEach(v => { if (v > maxVal) maxVal = v; }));
    const rowTotals = new Map<string, number>();
    const colTotals = new Map<string, number>();
    topIds.forEach(src => {
        const rowSum = [...(matrix.get(src)?.values() || [])].reduce((a, b) => a + b, 0);
        rowTotals.set(src, rowSum);
    });
    topIds.forEach(tgt => {
        const colSum = topIds.reduce((sum, src) => sum + (matrix.get(src)?.get(tgt) || 0), 0);
        colTotals.set(tgt, colSum);
    });
    const getName = (id: string) => {
        const n = nodes.find(n => n.id === id);
        const raw = n ? (n.name || n.id) : id;
        return raw.length > 14 ? raw.slice(0, 13) + '…' : raw;
    };
    const getFullName = (id: string) => {
        const n = nodes.find(n => n.id === id);
        return n ? (n.name || n.id) : id;
    };
    return { topIds, matrix, maxVal, rowTotals, colTotals, getName, getFullName };
}

/**
 * Renders a directed Matrix Heatmap (X→Y request volume) using SVG.
 */
function renderMatrixSVG(container: HTMLElement, nodes: any[], links: any[]): void {
    if (!container || !Array.isArray(links) || links.length === 0) return;
    const { topIds, matrix, maxVal, rowTotals, colTotals, getName, getFullName } = buildMatrixData(nodes, links, TOP_N);
    if (maxVal === 0) return;

    const containerW = container.clientWidth || 640;
    const N = topIds.length;
    const cellSize = Math.max(18, Math.min(36, Math.floor((containerW - MATRIX_LABEL_W - 12) / N)));
    const svgW = MATRIX_LABEL_W + N * cellSize + 8;
    const svgH = MATRIX_TITLE_H + MATRIX_LABEL_H + N * cellSize + 8;

    const svg = createSVGElement('svg', {
        width: '100%', height: svgH,
        viewBox: `0 0 ${svgW} ${svgH}`,
        preserveAspectRatio: 'xMinYMin meet'
    });

    const titleEl = createSVGElement('text', { x: 0, y: 18, fill: 'var(--text-dim)', style: 'font-size:0.7rem;font-weight:600;' });
    titleEl.textContent = `요청 흐름 행렬 (상위 ${N}명 · 행=발신 · 열=수신)`;
    svg.appendChild(titleEl);

    const offsetY = MATRIX_TITLE_H;

    topIds.forEach((tgt, j) => {
        const x = MATRIX_LABEL_W + j * cellSize + cellSize / 2;
        const y = offsetY + MATRIX_LABEL_H - 6;
        const text = createSVGElement('text', {
            x: 0, y: 0, transform: `translate(${x},${y}) rotate(-45)`,
            'text-anchor': 'start', fill: 'var(--text-dim)', style: 'font-size:0.62rem;'
        });
        text.textContent = getName(tgt);
        svg.appendChild(text);
    });

    topIds.forEach((src, i) => {
        const rowY = offsetY + MATRIX_LABEL_H + i * cellSize;
        const lbl = createSVGElement('text', {
            x: MATRIX_LABEL_W - 8, y: rowY + cellSize / 2,
            'text-anchor': 'end', 'dominant-baseline': 'middle',
            fill: 'var(--text-main)', style: 'font-size:0.65rem;'
        });
        lbl.textContent = getName(src);
        svg.appendChild(lbl);

        topIds.forEach((tgt, j) => {
            const val = matrix.get(src)?.get(tgt) || 0;
            const cx = MATRIX_LABEL_W + j * cellSize;
            const opacity = val === 0 ? 0.06 : Math.max(0.12, (val / maxVal) * 0.88);
            const cell = createSVGElement('rect', {
                x: cx + 1, y: rowY + 1, width: cellSize - 2, height: cellSize - 2, rx: 2,
                fill: val === 0 ? 'var(--text-dim)' : 'var(--color-primary)',
                'fill-opacity': opacity
            });
            if (val > 0) {
                const srcName = getFullName(src);
                const tgtName = getFullName(tgt);
                const rowPct = Math.round(val / (rowTotals.get(src) || 1) * 100);
                const colPct = Math.round(val / (colTotals.get(tgt) || 1) * 100);
                const tip = `<b>${escapeHTML(srcName)} → ${escapeHTML(tgtName)}</b><br/>${val}건<br/><span style="color:var(--text-dim)">${srcName} 발신의 ${rowPct}% &nbsp;·&nbsp; ${tgtName} 수신의 ${colPct}%</span>`;
                cell.addEventListener('mousemove', (e) => showTooltip(e as MouseEvent, tip));
                cell.addEventListener('mouseleave', () => hideTooltip());
            }
            svg.appendChild(cell);

            if (val > 0 && cellSize >= 22) {
                const num = createSVGElement('text', {
                    x: cx + cellSize / 2, y: rowY + cellSize / 2,
                    'text-anchor': 'middle', 'dominant-baseline': 'middle',
                    fill: 'var(--text-main)',
                    style: `font-size:${cellSize >= 28 ? '0.6' : '0.5'}rem;pointer-events:none;opacity:0.75;`
                });
                num.textContent = String(val);
                svg.appendChild(num);
            }
        });
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
        const matrixChartArea = document.getElementById('reportMatrixChart');

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
            if (matrixChartArea && matrixChartArea.dataset.vizKey !== vizKey) {
                matrixChartArea.dataset.vizKey = vizKey;
                requestAnimationFrame(() => { matrixChartArea.innerHTML = ''; renderMatrixSVG(matrixChartArea, viz.nodes, viz.links); });
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
