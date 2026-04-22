/**
 * src/insightsRenderer.ts
 * Refactored to a Passive View architecture.
 * Removes all direct dependencies on global state and i18n data.
 * All data and localization must be injected by the controller.
 */

import { IReportData, UserStats, TokenUsage } from './types';
import { generateHeatmapData } from './logic';
import { reportsRenderer } from './renderers/reports-renderer';

const getCssVariableValue = (varName: string): string => {
    return getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
};

const SVG_NS = 'http://www.w3.org/2000/svg';
function createSVG(tag: string, attrs: Record<string, string | number>): SVGElement {
    const el = document.createElementNS(SVG_NS, tag);
    Object.entries(attrs).forEach(([k, v]) => el.setAttribute(k, String(v)));
    return el;
}

export const insightsRenderer = {
    /**
     * Updates AI consumption slots.
     */
    renderTokenUsage(usage: TokenUsage | null, i18n: any): void {
        const slot = document.getElementById('ai-usage-consolidated');
        if (!slot) return;
        slot.innerHTML = '';

        const { 
            todayTotal = 0, todayPrompt = 0, todayCompletion = 0,
            monthlyTotal = 0, monthlyPrompt = 0, monthlyCompletion = 0,
            monthlyCost = 0, model = 'Gemini 3 Flash' 
        } = usage || {};
        
        const costStr = typeof monthlyCost === 'number' ? `$${monthlyCost.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}` : '$0.00';

        slot.innerHTML = `
            <span class="stat-card__label">${i18n.tokenUsageTitle || '토큰 사용량'}</span>
            <div class="c-ai-usage">
                <div class="c-ai-usage__item">
                    <span class="c-ai-usage__value">${todayTotal.toLocaleString()}</span>
                    <span class="c-ai-usage__detail">입 ${todayPrompt.toLocaleString()} / 출 ${todayCompletion.toLocaleString()}</span>
                    <span class="c-ai-usage__info">${i18n.todayAIUsage || '오늘 AI 사용'}</span>
                </div>
                <div class="c-ai-usage__item">
                    <span class="c-ai-usage__value">${monthlyTotal.toLocaleString()}</span>
                    <span class="c-ai-usage__detail">입 ${monthlyPrompt.toLocaleString()} / 출 ${monthlyCompletion.toLocaleString()}</span>
                    <span class="c-ai-usage__info">${i18n.monthlyAIUsage || '이번 달 AI 사용'}</span>
                </div>
                <div class="c-ai-usage__item">
                    <span class="c-ai-usage__value">${costStr}</span>
                    <span class="c-ai-usage__info">${i18n.estimatedCost || '추정 비용'}</span>
                    <span class="c-ai-usage__detail">${model}</span>
                </div>
            </div>
        `;
    },

    /**
     * Updates Noise Rejection slots.
     */
    renderFilteredNoise(usage: TokenUsage | null, i18n: any): void {
        const slot = document.getElementById('ai-noise-filtered');
        if (!slot) return;
        slot.innerHTML = '';

        const { todayFiltered = 0, monthlyFiltered = 0 } = usage || {};

        slot.innerHTML = `
            <span class="stat-card__label">${i18n.noiseFiltered || '노이즈 필터링'}</span>
            <div class="c-ai-usage">
                <div class="c-ai-usage__item">
                    <span class="c-ai-usage__value">${todayFiltered.toLocaleString()}</span>
                    <span class="c-ai-usage__info">${i18n.filteredToday || '오늘 차단'}</span>
                </div>
                <div class="c-ai-usage__item">
                    <span class="c-ai-usage__value">${monthlyFiltered.toLocaleString()}</span>
                    <span class="c-ai-usage__info">${i18n.filteredMonthly || '이번 달 차단'}</span>
                </div>
            </div>
        `;
    },

    /**
     * Updates daily performance slots.
     */
    renderDailyGlance(stats: UserStats | null, i18n: any): void {
        const valSlot = document.getElementById('dailyGlanceValue');
        const detSlot = document.getElementById('dailyGlanceDetail');
        if (!valSlot || !stats) return;
        valSlot.innerHTML = '';
        if (detSlot) detSlot.innerHTML = '';

        const completed = stats.total_completed ?? 0;
        const historyLen = stats.completion_history?.length ?? 0;
        const avg = historyLen > 0 ? (completed / historyLen).toFixed(1) : '0';
        
        valSlot.innerHTML = `
            <span class="stat-card__label">${i18n.completedTasks || '완료 업무'}</span>
            <div class="stat-card__multi-value">
                <span class="c-insights-card__main-value">${i18n.totalCompleted || '누적'} ${completed}</span>
                <span class="stat-card__secondary-value">${i18n.averageDaily || '일 평균'} ${avg}</span>
            </div>
        `;
        
        if (detSlot) {
            detSlot.innerHTML = '';
        }
    },

    /**
     * Updates stale tasks count.
     */
    renderStaleTasks(stats: UserStats | null, i18n: any): void {
        const slot = document.getElementById('staleTasksValue');
        if (!slot) return;
        slot.innerHTML = '';
        
        const staleCount = stats?.abandoned_tasks ?? 0;
        
        slot.innerHTML = `
            <span class="stat-card__label">${i18n.staleTasks || '방치된 업무'}</span>
            <div class="c-insights-card__main-value">${staleCount}</div>
        `;
    },


    /**
     * Updates channel distribution slot with a Pie Chart.
     */
    renderChannelDistribution(stats: any, i18n: any): void {
        const container = document.getElementById('source-distribution-slot');
        if (!container) return;
        const dist = stats.source_distribution_total || stats.source_distribution || {};
        const entries = Object.entries(dist).map(([name, value]) => ({ 
            name: name.charAt(0).toUpperCase() + name.slice(1), 
            value: Number(value)
        })).filter(e => e.value > 0);

        if (entries.length === 0) {
            container.innerHTML = `<div class="u-text-dim u-p-4">${i18n.noResults || 'No data'}</div>`;
            return;
        }

        container.innerHTML = `
            <div id="sourceDistributionChart" class="u-w-full u-flex u-flex-center" style="height: 11.25rem;"></div>
            <span class="stat-card__label">${i18n.sourceDistribution || '소스별 비중'}</span>
        `;
        const chartNode = document.getElementById('sourceDistributionChart');
        if (!chartNode) return;

        const svg = createSVG('svg', { viewBox: '0 0 100 100', width: '100%', height: '100%' });
        let currentAngle = 0;
        const colors = [getCssVariableValue('--color-slack'), getCssVariableValue('--color-whatsapp'), getCssVariableValue('--color-gmail'), getCssVariableValue('--color-warning'), getCssVariableValue('--color-purple')];
        const total = entries.reduce((sum, e) => sum + e.value, 0);

        const showPieTooltip = (name: string, value: number, pct: number, ev: MouseEvent) => {
            let t = document.getElementById('pie-tooltip');
            if (!t) {
                t = document.createElement('div');
                t.id = 'pie-tooltip';
                t.className = 'c-insights-tooltip';
                document.body.appendChild(t);
            }
            t.innerHTML = `<strong>${name}</strong><br/>${value.toLocaleString()}건 &nbsp; ${pct}%`;
            t.style.left = (ev.pageX + 15) + 'px';
            t.style.top = (ev.pageY + 15) + 'px';
            t.classList.add('c-insights-tooltip--active');
        };
        const hidePieTooltip = () => {
            const t = document.getElementById('pie-tooltip');
            if (t) t.classList.remove('c-insights-tooltip--active');
        };

        entries.forEach((e, i) => {
            const p = e.value / total;
            const x1 = 50 + 40 * Math.cos(currentAngle);
            const y1 = 50 + 40 * Math.sin(currentAngle);
            currentAngle += p * 2 * Math.PI;
            const x2 = 50 + 40 * Math.cos(currentAngle);
            const y2 = 50 + 40 * Math.sin(currentAngle);
            const largeArc = p > 0.5 ? 1 : 0;
            const d = `M 50 50 L ${x1} ${y1} A 40 40 0 ${largeArc} 1 ${x2} ${y2} Z`;
            const path = createSVG('path', { d, fill: colors[i % colors.length], stroke: 'var(--card-bg)', 'stroke-width': 1 });
            const pct = (p * 100).toFixed(1);
            path.style.cursor = 'pointer';
            path.addEventListener('mousemove', (ev) => showPieTooltip(e.name, e.value, Number(pct), ev as MouseEvent));
            path.addEventListener('mouseleave', hidePieTooltip);
            svg.appendChild(path);
        });
        svg.appendChild(createSVG('circle', { cx: 50, cy: 50, r: 25, fill: 'var(--bg-color)' }));
        chartNode.appendChild(svg);
    },

    renderHourlyActivity(stats: UserStats | null, i18n: any): void {
        const container = document.getElementById('stat-peak');
        if (!container || !stats?.hourly_activity) return;
        container.innerHTML = '';
        
        const activity = stats.hourly_activity;
        const maxCount = Math.max(...Object.values(activity), 1);
        const tierClasses = ['', 'c-peak-chart__bar--tier-1', 'c-peak-chart__bar--tier-2', 'c-peak-chart__bar--tier-3', 'c-peak-chart__bar--tier-4'];
        
        const bars = Array.from({ length: 24 }, (_, i) => {
            const count = activity[i.toString()] || 0;
            const height = (count / maxCount) * 100;
            const tier = count > 0 ? Math.min(4, Math.ceil((count / maxCount) * 4)) : 0;
            return `<div class="c-peak-chart__bar ${tierClasses[tier]}" style="height: ${height}%" title="${i}시: ${count}"></div>`;
        });

        const labels = Array.from({ length: 24 }, (_, i) => 
            `<span class="c-peak-chart__label">${i}</span>`
        ).join('');

        container.innerHTML = `
            <div class="c-peak-chart">
                <div class="c-peak-chart__bars">${bars.join('')}</div>
                <div class="c-peak-chart__labels">${labels}</div>
            </div>
            <span class="stat-card__label">${i18n.peakTime || '피크 타임'}</span>
        `;
    },

    renderActivityHeatmap(stats: any, i18n: any, targetId: string = 'activity-heatmap-slot'): void {
        const container = document.getElementById(targetId);
        if (!container) return;
        container.innerHTML = '';
        const history = stats?.completion_history;
        if (!history || history.length === 0) {
            container.innerHTML = `<div class="heatmap-widget--empty">${i18n.noResults || 'No activity'}</div>`;
            return;
        }

        const heatmapData = generateHeatmapData(history, 91);
        const cells = heatmapData.map((d: any) => {
            const tier = d.level > 0 ? `heatmap-grid__cell--tier-${d.level}` : '';
            const cStr = JSON.stringify(d.counts).replace(/"/g, '&quot;');
            return `<div class="heatmap-grid__cell ${tier}" data-date="${d.date}" data-count="${d.count}" data-counts="${cStr}"></div>`;
        });

        const label = targetId === 'stat-peak' ? (i18n.peakTime || '피크 타임') : (i18n.recentActivity91 || '최근 활동 (91일)');

        container.innerHTML = `
            <div class="heatmap-widget">
                <div class="heatmap-grid">${cells.join('')}</div>
            </div>
            <span class="stat-card__label">${label}</span>
        `;
        this.bindHeatmapEvents(container);
    },

    bindHeatmapEvents(container: HTMLElement): void {
        container.addEventListener('mousemove', (e: MouseEvent) => {
            const cell = (e.target as HTMLElement).closest('.heatmap-grid__cell') as HTMLElement;
            if (cell) this.updateTooltip(cell, e);
        });
        container.addEventListener('mouseleave', () => {
            const t = document.getElementById('heatmap-tooltip');
            if (t) t.classList.remove('c-insights-tooltip--active');
        });
    },

    updateTooltip(cell: HTMLElement, e: MouseEvent): void {
        let t = document.getElementById('heatmap-tooltip');
        if (!t) {
            t = document.createElement('div');
            t.id = 'heatmap-tooltip';
            t.className = 'c-insights-tooltip';
            document.body.appendChild(t);
        }
        const counts = JSON.parse(cell.dataset.counts || '{}');
        const detail = Object.entries(counts).map(([k, v]) => `${k}:${v}`).join(', ');
        
        t.innerHTML = `<strong>${cell.dataset.date}</strong><br/>Total: ${cell.dataset.count}<br/><small>${detail}</small>`;
        const offsetVal = 15;
        t.style.left = (e.pageX + offsetVal) + 'px';
        t.style.top = (e.pageY + offsetVal) + 'px';
        t.classList.add('c-insights-tooltip--active');
    },

    renderCompletionTrend(stats: any, days: number): void {
        const container = document.getElementById('ankiChartContainer');
        if (!container) return;
        container.innerHTML = '';

        const rawHistory = stats?.completion_history;
        if (!rawHistory || rawHistory.length === 0) {
            container.innerHTML = `<div class="u-text-dim" style="padding:2rem;text-align:center;">데이터 없음</div>`;
            return;
        }

        const history = (rawHistory as any[]).slice(-days);
        if (history.length === 0) {
            container.innerHTML = `<div class="u-text-dim" style="padding:2rem;text-align:center;">데이터 없음</div>`;
            return;
        }

        // counts: Record<string,number> → daily total
        const totals = history.map((d: any) => ({
            date: String(d.date ?? ''),
            total: Object.values(d.counts ?? {}).reduce((a: number, b) => a + (b as number), 0)
        }));

        const W = 800, H = 200;
        const PAD = { top: 16, right: 16, bottom: 28, left: 36 };
        const iW = W - PAD.left - PAD.right;
        const iH = H - PAD.top - PAD.bottom;
        const maxVal = Math.max(...totals.map(d => d.total), 1);
        const xStep = iW / (totals.length - 1 || 1);
        const toX = (i: number) => PAD.left + i * xStep;
        const toY = (v: number) => PAD.top + iH - (v / maxVal) * iH;
        const pts = totals.map((d, i) => ({ x: toX(i), y: toY(d.total), date: d.date, total: d.total }));

        const svg = createSVG('svg', { viewBox: `0 0 ${W} ${H}`, width: '100%', height: '180' });

        // gradient def
        const defs = createSVG('defs', {});
        const grad = createSVG('linearGradient', { id: 'ankiAreaGrad', x1: '0', y1: '0', x2: '0', y2: '1' });
        [['0%', '0.25'], ['100%', '0']].forEach(([offset, opacity]) => {
            const s = createSVG('stop', { offset, 'stop-color': 'var(--accent-color)', 'stop-opacity': opacity });
            grad.appendChild(s);
        });
        defs.appendChild(grad);
        svg.appendChild(defs);

        // horizontal grid lines + y labels
        [0.25, 0.5, 0.75, 1].forEach(ratio => {
            const y = PAD.top + iH * (1 - ratio);
            svg.appendChild(createSVG('line', {
                x1: PAD.left, y1: y, x2: PAD.left + iW, y2: y,
                stroke: 'var(--border-color)', 'stroke-width': 0.5, 'stroke-dasharray': '4 4'
            }));
            const lbl = createSVG('text', { x: PAD.left - 4, y: y + 4, 'text-anchor': 'end', 'font-size': 9, fill: 'var(--text-dim)' });
            lbl.textContent = String(Math.round(maxVal * ratio));
            svg.appendChild(lbl);
        });

        // area fill
        const baseY = PAD.top + iH;
        const areaD = `M ${pts[0].x},${baseY} L ${pts.map(p => `${p.x},${p.y}`).join(' L ')} L ${pts[pts.length - 1].x},${baseY} Z`;
        svg.appendChild(createSVG('path', { d: areaD, fill: 'url(#ankiAreaGrad)' }));

        // line
        svg.appendChild(createSVG('path', {
            d: `M ${pts.map(p => `${p.x},${p.y}`).join(' L ')}`,
            fill: 'none', stroke: 'var(--accent-color)', 'stroke-width': 2
        }));

        // x-axis date labels (up to 6 evenly spaced)
        const labelCount = Math.min(6, totals.length);
        const labelIdxStep = (totals.length - 1) / (labelCount - 1 || 1);
        for (let li = 0; li < labelCount; li++) {
            const i = Math.round(li * labelIdxStep);
            const p = pts[i];
            const lbl = createSVG('text', { x: p.x, y: H - 4, 'text-anchor': 'middle', 'font-size': 9, fill: 'var(--text-dim)' });
            lbl.textContent = p.date.slice(5); // MM-DD
            svg.appendChild(lbl);
        }

        // dots + transparent hit rects for tooltip
        pts.forEach(p => {
            const hitW = Math.max(xStep, 20);
            const hit = createSVG('rect', { x: p.x - hitW / 2, y: PAD.top, width: hitW, height: iH, fill: 'transparent' });
            hit.style.cursor = 'crosshair';
            hit.addEventListener('mousemove', (ev) => {
                let t = document.getElementById('anki-tooltip');
                if (!t) { t = document.createElement('div'); t.id = 'anki-tooltip'; t.className = 'c-insights-tooltip'; document.body.appendChild(t); }
                t.innerHTML = `<strong>${p.date}</strong><br/>${p.total.toLocaleString()}건`;
                t.style.left = ((ev as MouseEvent).pageX + 15) + 'px';
                t.style.top = ((ev as MouseEvent).pageY + 15) + 'px';
                t.classList.add('c-insights-tooltip--active');
            });
            hit.addEventListener('mouseleave', () => { document.getElementById('anki-tooltip')?.classList.remove('c-insights-tooltip--active'); });
            svg.appendChild(hit);
            svg.appendChild(createSVG('circle', { cx: p.x, cy: p.y, r: 3, fill: 'var(--accent-color)' }));
        });

        container.appendChild(svg);
    },

    /**
     * Initializes the report list UI.
     * Logic for fetching and auto-loading moved to Controller.
     */
    renderReportList(history: IReportData[], i18n: any, activeId: number | null = null): void {
        const container = document.getElementById('reportList');
        if (!container) return;

        // reports가 배열이 아니거나 비었을 때의 방어 로직 강화
        if (!Array.isArray(history) || history.length === 0) {
            container.innerHTML = `<div class="u-text-dim" style="padding: 1rem; text-align: center;">${i18n.insights.no_reports || '사용 가능한 보고서가 없습니다.'}</div>`;
            return;
        }

        reportsRenderer.renderHistory(container, history, (selected) => {
            (window as any).insights.loadExistingReport(selected);
        }, i18n);

        // UI auto-selection logic
        const target = activeId 
            ? history.find(r => r.id === activeId) 
            : history[0];
            
        if (target) {
            const index = activeId ? history.indexOf(target) : 0;
            const items = container.querySelectorAll('.c-insights-report-item');
            if (items[index]) (items[index] as HTMLElement).classList.add('c-insights-report-item--active');
        }
    },

    /**
     * Renders the detail of a single report.
     */
    renderReport(report: IReportData, lang: string, i18n: any): void {
        const detailContainer = document.querySelector('.c-insights-report-main') as HTMLElement;
        if (!detailContainer || !report) return;

        reportsRenderer.render(report, lang, i18n);
    },

    renderLoading(container: HTMLElement, i18n: any, type: 'report' | 'translation' | 'load' = 'report'): void {
        let msg = i18n.generatingReport || "AI 리포트 분석 중...";
        
        if (type === 'translation') msg = i18n.generatingTranslation || "AI 번역 생성 중...";
        if (type === 'load') msg = i18n.loadingData || "데이터를 불러오는 중...";

        container.innerHTML = `<div class="c-report-loading u-p-8"><div class="spinner"></div><p class="u-mt-4">${msg}</p></div>`;
    },

    /**
     * Renders empty state when no report is selected or exists.
     */
    renderEmptyState(i18n: any): void {
        const summaryContent = document.getElementById('reportSummaryContent');
        if (!summaryContent) return;

        summaryContent.innerHTML = `
            <div class="c-reports-empty-state u-p-8 u-text-center">
                <div class="u-text-6xl u-mb-4">📊</div>
                <h3 class="u-text-xl u-font-bold u-mb-2">${i18n.noReportsYet || '생성된 리포트가 없습니다'}</h3>
                <p class="u-text-dim">${i18n.generateReportDesc || 'AI를 통해 오늘 업무 리포트를 생성해 보세요.'}</p>
            </div>
        `;

        const netChart = document.getElementById('reportNetworkChart');
        const sankeyChart = document.getElementById('reportSankeyChart');
        if (netChart) netChart.innerHTML = '';
        if (sankeyChart) sankeyChart.innerHTML = '';
    },

    renderError(container: HTMLElement, message: string, i18n: any): void {
        const retryMsg = i18n.retryLanguageSelection || "다시 한 번 언어를 선택해 주세요";
        container.innerHTML = `
            <div class="c-report-error u-p-8 u-text-error">
                ${message}
                <div class="u-mt-2 u-text-dim u-text-xs">${retryMsg}</div>
            </div>`;
    }
};
