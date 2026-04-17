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
    Object.entries(attrs).forEach(([key, val]) => {
        // Only set as attribute if value is not undefined
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
    if (!container || !Array.isArray(nodes) || !Array.isArray(links)) {
        console.warn("[ReportsRenderer] Skipping network rendering: Missing or invalid data.");
        return;
    }
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });
    const coords = calculateCircularLayout(nodes, width / 2, height / 2, Math.min(width, height) / 2 - 60);

    // 노드(사용자)의 value 값이 없을 경우 선(연결)의 합산 가중치로 계산
    nodes.forEach(n => {
        if (n.value === undefined) {
            n.value = links.reduce((sum, l) => sum + (l.source === n.id || l.target === n.id ? (l.value || 1) : 0), 0);
        }
    });

    const nodeShapes = new Map<string, SVGElement>();

    links.forEach(l => {
        const s = coords.get(l.source);
        const t = coords.get(l.target);
        if (s && t) {
            const sourceName = getNodeName(nodes, l.source);
            const targetName = getNodeName(nodes, l.target);
            const line = createSVGElement('line', {
                x1: s.x, y1: s.y, x2: t.x, y2: t.y,
                class: 'c-report-viz__link',
                'stroke-width': Math.max(1, (l.value || 1) * 2),
                stroke: 'var(--color-primary)',
                opacity: '0.4'
            });
            line.addEventListener('mousemove', (e: Event) => showTooltip(e as MouseEvent, `<b>${sourceName} ↔ ${targetName}</b><br/>연결 강도: ${l.value || 1}`));
            line.addEventListener('mouseenter', () => {
                line.setAttribute('opacity', '1');
                line.setAttribute('stroke', 'var(--color-warning)');
                const sNode = nodeShapes.get(l.source);
                const tNode = nodeShapes.get(l.target);
                if (sNode) { sNode.setAttribute('stroke', 'var(--color-warning)'); sNode.setAttribute('stroke-width', '3'); }
                if (tNode) { tNode.setAttribute('stroke', 'var(--color-warning)'); tNode.setAttribute('stroke-width', '3'); }
            });
            line.addEventListener('mouseout', () => {
                hideTooltip();
                line.setAttribute('opacity', '0.4');
                line.setAttribute('stroke', 'var(--color-primary)');
                const sNode = nodeShapes.get(l.source);
                const tNode = nodeShapes.get(l.target);
                if (sNode) { sNode.removeAttribute('stroke'); sNode.removeAttribute('stroke-width'); }
                if (tNode) { tNode.removeAttribute('stroke'); tNode.removeAttribute('stroke-width'); }
            });
            svg.appendChild(line);
        }
    });

    nodes.forEach(n => {
        const p = coords.get(n.id);
        if (p) {
            const radius = Math.max(6, 4 + Math.sqrt(n.value || 1) * 3);
            const g = createSVGElement('g', { class: 'c-report-viz__node-group' });

            const circle = createSVGElement('circle', {
                cx: p.x, cy: p.y, r: radius,
                class: `c-report-viz__node ${n.is_me ? 'c-report-viz__node--me' : ''}`,
                fill: n.is_me ? 'var(--color-primary)' : 'var(--color-info)'
            });

            nodeShapes.set(n.id, circle);

            g.addEventListener('mousemove', (e: Event) => showTooltip(e as MouseEvent, `<b>${n.name || n.id}</b><br/>총 업무 관여: ${n.value}`));
            g.addEventListener('mouseenter', () => {
                circle.setAttribute('stroke', 'var(--color-warning)');
                circle.setAttribute('stroke-width', '3');
            });
            g.addEventListener('mouseout', () => {
                hideTooltip();
                circle.removeAttribute('stroke');
                circle.removeAttribute('stroke-width');
            });
            g.appendChild(circle);

            const text = createSVGElement('text', {
                x: p.x, y: p.y + radius + 14,
                'text-anchor': 'middle',
                fill: 'var(--text-main)',
                style: `font-size: 0.75rem; font-weight: ${n.is_me ? 'bold' : 'normal'}`
            });
            text.textContent = n.name || n.id;
            g.appendChild(text);

            svg.appendChild(g);
        }
    });
    container.appendChild(svg);
}

/**
 * Renders a Sankey Chart using SVG Cubic Bezier paths.
 */
function renderSankeySVG(container: HTMLElement, nodes: any[], links: any[]): void {
    if (!container || !Array.isArray(nodes) || !Array.isArray(links)) {
        console.warn("[ReportsRenderer] Skipping sankey rendering: Missing or invalid data.");
        return;
    }
    const width = container.clientWidth || 800;
    const height = container.clientHeight || 400;
    const svg = createSVGElement('svg', { width: '100%', height: '100%', viewBox: `0 0 ${width} ${height}` });

    // 노드(사용자)의 value 값이 없을 경우 선(연결)의 합산 가중치로 계산
    nodes.forEach(n => {
        if (n.value === undefined) {
            n.value = links.reduce((sum, l) => sum + (l.source === n.id || l.target === n.id ? (l.value || 1) : 0), 0);
        }
    });

    const sources = nodes.filter(n => links.some(l => l.source === n.id));
    const targets = nodes.filter(n => links.some(l => l.target === n.id && !sources.includes(n)));

    const sCoords = calculateColumnLayout(sources, 120, height);
    const tCoords = calculateColumnLayout(targets, width - 120, height);

    const nodeShapes = new Map<string, SVGElement>();

    links.forEach(l => {
        const s = sCoords.get(l.source);
        const t = tCoords.get(l.target);
        if (!s || !t) return;
        const strokeWidth = Math.max(2, (l.value || 1) * 2);
        const d = `M ${s.x} ${s.y} C ${(s.x + t.x) / 2} ${s.y}, ${(s.x + t.x) / 2} ${t.y}, ${t.x} ${t.y}`;
        const sourceName = getNodeName(nodes, l.source);
        const targetName = getNodeName(nodes, l.target);
        const path = createSVGElement('path', {
            d,
            class: 'c-report-viz__link',
            fill: 'none',
            'stroke-width': strokeWidth,
            stroke: 'var(--color-primary)',
            opacity: '0.4'
        });
        path.addEventListener('mousemove', (e: Event) => showTooltip(e as MouseEvent, `<b>${sourceName} → ${targetName}</b><br/>흐름 강도: ${l.value || 1}`));
        path.addEventListener('mouseenter', () => {
            path.setAttribute('opacity', '1');
            path.setAttribute('stroke', 'var(--color-warning)');
            const sNode = nodeShapes.get(l.source);
            const tNode = nodeShapes.get(l.target);
            if (sNode) { sNode.setAttribute('stroke', 'var(--color-warning)'); sNode.setAttribute('stroke-width', '3'); }
            if (tNode) { tNode.setAttribute('stroke', 'var(--color-warning)'); tNode.setAttribute('stroke-width', '3'); }
        });
        path.addEventListener('mouseout', () => {
            hideTooltip();
            path.setAttribute('opacity', '0.4');
            path.setAttribute('stroke', 'var(--color-primary)');
            const sNode = nodeShapes.get(l.source);
            const tNode = nodeShapes.get(l.target);
            if (sNode) { sNode.removeAttribute('stroke'); sNode.removeAttribute('stroke-width'); }
            if (tNode) { tNode.removeAttribute('stroke'); tNode.removeAttribute('stroke-width'); }
        });
        svg.appendChild(path);
    });

    [...sCoords.values()].forEach((p, i) => {
        const n = sources[i];
        const h = Math.max(16, 10 + Math.sqrt(n.value || 1) * 4);
        const g = createSVGElement('g', { class: 'c-report-viz__node-group' });

        const rect = createSVGElement('rect', {
            x: p.x - 4, y: p.y - h / 2, width: 8, height: h,
            class: 'c-report-viz__node', rx: 2,
            fill: n.is_me ? 'var(--color-primary)' : 'var(--color-info)'
        });

        nodeShapes.set(n.id, rect);

        g.addEventListener('mousemove', (e: Event) => showTooltip(e as MouseEvent, `<b>${n.name || n.id}</b><br/>총 업무 관여: ${n.value}`));
        g.addEventListener('mouseenter', () => {
            rect.setAttribute('stroke', 'var(--color-warning)');
            rect.setAttribute('stroke-width', '3');
        });
        g.addEventListener('mouseout', () => {
            hideTooltip();
            rect.removeAttribute('stroke');
            rect.removeAttribute('stroke-width');
        });
        g.appendChild(rect);

        const text = createSVGElement('text', {
            x: p.x - 12, y: p.y + 4,
            'text-anchor': 'end',
            fill: 'var(--text-main)',
            style: `font-size: 0.75rem; font-weight: ${n.is_me ? 'bold' : 'normal'}`
        });
        text.textContent = n.name || n.id;
        g.appendChild(text);
        svg.appendChild(g);
    });

    [...tCoords.values()].forEach((p, i) => {
        const n = targets[i];
        const h = Math.max(16, 10 + Math.sqrt(n.value || 1) * 4);
        const g = createSVGElement('g', { class: 'c-report-viz__node-group' });

        const rect = createSVGElement('rect', {
            x: p.x - 4, y: p.y - h / 2, width: 8, height: h,
            class: 'c-report-viz__node', rx: 2,
            fill: n.is_me ? 'var(--color-primary)' : 'var(--color-info)'
        });

        nodeShapes.set(n.id, rect);

        g.addEventListener('mousemove', (e: Event) => showTooltip(e as MouseEvent, `<b>${n.name || n.id}</b><br/>총 업무 관여: ${n.value}`));
        g.addEventListener('mouseenter', () => {
            rect.setAttribute('stroke', 'var(--color-warning)');
            rect.setAttribute('stroke-width', '3');
        });
        g.addEventListener('mouseout', () => {
            hideTooltip();
            rect.removeAttribute('stroke');
            rect.removeAttribute('stroke-width');
        });
        g.appendChild(rect);

        const text = createSVGElement('text', {
            x: p.x + 12, y: p.y + 4,
            'text-anchor': 'start',
            fill: 'var(--text-main)',
            style: `font-size: 0.75rem; font-weight: ${n.is_me ? 'bold' : 'normal'}`
        });
        text.textContent = n.name || n.id;
        g.appendChild(text);
        svg.appendChild(g);
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
            
            let statusTag = '';
            if (item.status === 'processing') {
                statusTag = `<span class="c-insights-report-item__status c-insights-report-item__status--processing">⌛ ${i18n.generating || '생성 중...'}</span>`;
            } else if (item.status === 'failed') {
                statusTag = `<span class="c-insights-report-item__status c-insights-report-item__status--failed">⚠️ ${i18n.error || '실패'}</span>`;
            } else if (item.status === 'completed') {
                statusTag = `<span class="c-insights-report-item__status c-insights-report-item__status--completed">✅</span>`;
            }

            btn.innerHTML = `
                <div class="c-insights-report-item__content">
                    <div class="c-insights-report-item__info">
                        <span class="c-insights-report-item__date">${item.start_date} ~ ${item.end_date}</span>
                        <div class="c-insights-report-item__title">
                            ${statusTag}
                            ${item.title || (i18n.weeklyReportTitle || '업무 요약 리포트')}
                        </div>
                    </div>
                </div>
                <button class="c-insights-report-item__delete" data-id="${item.id}" title="${i18n.delete || '삭제'}">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                </button>
            `;
            btn.onclick = (e) => {
                const target = e.target as HTMLElement;
                if (target.closest('.c-insights-report-item__delete')) return;
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

        // Robust Field mapping: translations > summary > report_summary
        const summaryText = report.translations?.[lang] || report.summary || report.report_summary || "";

        console.log("[ReportsRenderer] Rendering report:", {
            id: report.id,
            lang,
            summaryLength: summaryText.length,
            hasStalledTasks: summaryText.includes("## [Stalled Tasks]"),
            hasVizDataHeader: summaryText.includes("## [Visualization Data]"),
            vizNodesCount: report.visualization ? (typeof report.visualization === 'string' ? 'string' : (report.visualization as any).nodes?.length) : 0
        });

        if (summaryArea) {
            summaryArea.innerHTML = parseMarkdown(summaryText);
            // Ensure visualization section doesn't show raw JSON if parser failed to strip it
            if (summaryArea.innerHTML.includes("## [Visualization Data]")) {
                console.warn("[ReportsRenderer] Visualization Data header still present in HTML summary - check stripping logic.");
            }
        }

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
            let parsed: any;
            if (typeof vizRaw === 'string' && vizRaw.trim()) {
                parsed = JSON.parse(vizRaw);
            } else if (vizRaw && typeof vizRaw === 'object') {
                parsed = vizRaw;
            } else {
                parsed = { nodes: [], links: [] };
            }

            // Normalize Casing (Nodes/nodes, Links/links)
            viz = {
                nodes: parsed.nodes || parsed.Nodes || [],
                links: parsed.links || parsed.Links || []
            };
        } catch (e) {
            console.error("[ReportsRenderer] Visualization data parsing failed:", e);
            viz = { nodes: [], links: [] };
        }

        if (viz.nodes.length === 0) {
            if (netChartArea) netChartArea.innerHTML = `<div class="u-text-dim u-p-4">${i18n.noVizData || 'No visualization data available for this report.'}</div>`;
            if (sankeyChartArea) sankeyChartArea.innerHTML = '';
            return;
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
