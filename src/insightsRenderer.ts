/**
 * src/insightsRenderer.ts
 * Refactored to use slot-based partial updates instead of innerHTML overwrites. 
 * Preserves the layout and BEM structure defined in index.html.
 */

import { state, updateReportHistory, upsertReport } from './state.ts';
import { I18N_DATA } from './locales.js';
import { UserStats, TokenUsage, IReportData, IReportNode, IReportLink } from './types.ts';
import { generateHeatmapData, normalizeReportData } from './logic.ts';
import { reportsRenderer } from './renderers/reports-renderer.ts';
import { api } from './api.js';

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
    getI18n() {
        const lang = state.currentLang || 'ko';
        return I18N_DATA[lang as keyof typeof I18N_DATA] || I18N_DATA.ko;
    },

    resizeAll() {
        // Vanilla SVG charts using viewBox resize automatically with container.
    },


    /**
     * Updates AI consumption slots. 
     * Target: #aiConsumptionValue, #aiConsumptionDetail
     */
    renderTokenUsage(usage: TokenUsage | null): void {
        const slot = document.getElementById('ai-usage-consolidated');
        if (!slot) return;
        slot.innerHTML = '';

        const { 
            todayTotal = 0, todayPrompt = 0, todayCompletion = 0,
            monthlyTotal = 0, monthlyPrompt = 0, monthlyCompletion = 0,
            monthlyCost = 0, model = 'Gemini 3 Flash' 
        } = usage || {};
        
        const i18n = this.getI18n() as any;
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
     * Updates daily performance slots using BEM structure.
     * Target: #dailyGlanceValue, #dailyGlanceDetail
     */
    renderDailyGlance(stats: UserStats | null): void {
        const valSlot = document.getElementById('dailyGlanceValue');
        const detSlot = document.getElementById('dailyGlanceDetail');
        if (!valSlot || !stats) return;
        valSlot.innerHTML = '';
        if (detSlot) detSlot.innerHTML = '';

        const completed = stats.total_completed ?? 0;
        const historyLen = stats.completion_history?.length ?? 0;
        const avg = historyLen > 0 ? (completed / historyLen).toFixed(1) : '0';
        
        valSlot.innerHTML = `
            <span class="stat-card__label">완료 업무</span>
            <div class="stat-card__multi-value">
                <span class="c-insights-card__main-value">누적 ${completed}</span>
                <span class="stat-card__secondary-value">일 평균 ${avg}</span>
            </div>
        `;
        
        if (detSlot) {
            const waiting = stats.waiting_tasks ?? 0;
            detSlot.innerHTML = waiting > 0 
                ? `<span class="u-text-warning">⚠️ ${waiting} tasks waiting</span>` 
                : '';
        }
    },

    /**
     * Updates achievement slot.
     * Target: #achievementsList
     */
    renderAchievements(all: any[], user: any[], _stats: any): void {
        const container = document.getElementById('achievementsList');
        if (!container) return;
        container.innerHTML = '';

        const unlockedIds = (user || []).map((u: any) => u.achievement_id || u.id);
        const seriesItems = (all || []).filter(a => a.name.includes('태스크 마스터') || a.name.includes('첫 걸음'));
        const nonSeriesItems = (all || []).filter(a => !a.name.includes('태스크 마스터') && !a.name.includes('첫 걸음'));

        const itemsToDisplay: any[] = [];
        if (seriesItems.length > 0) {
            const firstLocked = seriesItems.find(s => !unlockedIds.includes(s.id));
            itemsToDisplay.push(firstLocked || seriesItems[seriesItems.length - 1]);
        }
        itemsToDisplay.push(...nonSeriesItems);

        // 컨텐츠와 라벨 표시
        container.innerHTML = `
            <div class="u-w-full">
                ${itemsToDisplay.slice(0, 2).map(ach => `
                    <div class="c-achievement u-mb-1">${ach.icon || '🏆'} ${ach.name}</div>
                `).join('')}
            </div>
            <span class="stat-card__label">최근 업적</span>
        `;
    },

    /**
     * Updates channel distribution slot with a Pie Chart.
     * Target: #sourceDistribution
     */
    renderChannelDistribution(stats: any): void {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;
        const dist = stats.source_distribution_total || stats.source_distribution || {};
        const entries = Object.entries(dist).map(([name, value]) => ({ 
            name: name.charAt(0).toUpperCase() + name.slice(1), 
            value: Number(value)
        })).filter(e => e.value > 0);

        if (entries.length === 0) {
            container.innerHTML = '<div class="u-text-dim u-p-4">No data</div>';
            return;
        }

        container.innerHTML = `
            <div id="sourceDistributionChart" class="u-w-full u-flex u-flex-center" style="height: 11.25rem;"></div>
            <span class="stat-card__label">소스별 비중</span>
        `;
        const chartNode = document.getElementById('sourceDistributionChart');
        if (!chartNode) return;

        const svg = createSVG('svg', { viewBox: '0 0 100 100', width: '100%', height: '100%' });
        let currentAngle = 0;
        const colors = [getCssVariableValue('--color-slack'), getCssVariableValue('--color-whatsapp'), getCssVariableValue('--color-gmail'), getCssVariableValue('--color-warning'), getCssVariableValue('--color-purple')];
        const total = entries.reduce((sum, e) => sum + e.value, 0);

        entries.forEach((e, i) => {
            const p = e.value / total;
            const x1 = 50 + 40 * Math.cos(currentAngle);
            const y1 = 50 + 40 * Math.sin(currentAngle);
            currentAngle += p * 2 * Math.PI;
            const x2 = 50 + 40 * Math.cos(currentAngle);
            const y2 = 50 + 40 * Math.sin(currentAngle);
            const largeArc = p > 0.5 ? 1 : 0;
            const d = `M 50 50 L ${x1} ${y1} A 40 40 0 ${largeArc} 1 ${x2} ${y2} Z`;
            svg.appendChild(createSVG('path', { d, fill: colors[i % colors.length], stroke: 'var(--card-bg)', 'stroke-width': 1 }));
        });
        svg.appendChild(createSVG('circle', { cx: 50, cy: 50, r: 25, fill: 'var(--bg-color)' })); // Donut center
        chartNode.appendChild(svg);
    },


    /**
     * Updates the 'Waiting' (대기 중) widget using backend-provided stats.
     * Target: #stat-stale
     */
    renderStaleTasks(stats: UserStats): void {
        const slot = document.getElementById('stat-stale');
        if (!slot) return;
        slot.innerHTML = '';

        const val = stats.waiting_tasks || 0;
        slot.innerHTML = `
            <span class="c-insights-card__main-value">${val}</span>
            <span class="stat-card__label">대기 중</span>
        `;
    },

    renderHourlyActivity(stats: UserStats | null): void {
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
            <span class="stat-card__label">피크 타임</span>
        `;
    },

    renderActivityHeatmap(stats: any, targetId: string = 'activityHeatmap'): void {
        const container = document.getElementById(targetId);
        if (!container) return;
        container.innerHTML = '';
        const history = stats?.completion_history;
        if (!history || history.length === 0) {
            container.innerHTML = `<div class="heatmap-widget--empty">No activity</div>`;
            return;
        }

        const heatmapData = generateHeatmapData(history, 91); // Polish: 13 weeks
        const cells = heatmapData.map((d: any) => {
            const tier = d.level > 0 ? `heatmap-grid__cell--tier-${d.level}` : '';
            const cStr = JSON.stringify(d.counts).replace(/"/g, '&quot;');
            return `<div class="heatmap-grid__cell ${tier}" data-date="${d.date}" data-count="${d.count}" data-counts="${cStr}"></div>`;
        });

        const label = targetId === 'stat-peak' ? '피크 타임' : '최근 활동 (91일)';

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

    renderAnkiChart(stats: any, days: number): void {
        const container = document.getElementById('ankiChartContainer');
        if (!container || !stats.completion_history) return;
        container.innerHTML = '';
        
        const history = (stats.completion_history || []).slice(-days);
        if (history.length === 0) return;

        const width = 800;
        const height = 200;
        const svg = createSVG('svg', { viewBox: `0 0 ${width} ${height}`, width: '100%', height: '100%' });
        
        const maxVal = Math.max(...history.map((d: any) => d.total || 0), 1);
        const padding = 20;
        const xStep = (width - padding * 2) / (history.length - 1 || 1);
        
        const points = history.map((d: any, i: number) => {
            const x = padding + i * xStep;
            const y = height - padding - ((d.total || 0) / maxVal) * (height - padding * 2);
            return { x, y };
        });

        const d = `M ${points.map((p: any) => `${p.x},${p.y}`).join(' L ')}`;
        svg.appendChild(createSVG('path', { d, fill: 'none', stroke: 'var(--accent-color)', 'stroke-width': 2 }));
        
        points.forEach((p: any) => {
            svg.appendChild(createSVG('circle', { cx: p.x, cy: p.y, r: 3, fill: 'var(--accent-color)' }));
        });

        container.appendChild(svg);
    },


    /**
     * Initializes the report list by fetching from API.
     */
    async initReportList(): Promise<void> {
        const container = document.getElementById('reportList');
        if (!container) return;

        try {
            const history = await api.fetchReportHistory();
            updateReportHistory(history);
            
            reportsRenderer.renderHistory(container, state.reportHistory, async (selected) => {
                const key = `${selected.start_date}_${selected.end_date}`;
                let report = state.reports[key];
                
                if (!report || !report.report_summary) {
                    this.renderLoading(document.querySelector('.c-insights-report-main') as HTMLElement);
                    const fullReport = await api.fetchReportDetail(selected.id!);
                    report = normalizeReportData(fullReport);
                    upsertReport(report);
                }
                
                this.renderReportDetail(report);
            });
        } catch (err) {
            console.error('Failed to load report history:', err);
            container.innerHTML = '<div class="u-text-dim u-p-4">Failed to load reports.</div>';
        }
    },

    /**
     * Renders the detail of a single report.
     */
    renderReportDetail(report: IReportData): void {
        const detailContainer = document.querySelector('.c-insights-report-main') as HTMLElement;
        if (detailContainer) {
            reportsRenderer.render(detailContainer, report);
        }
    },

    renderLoading(container: HTMLElement): void {
        const i18n = this.getI18n() as any;
        const msg = i18n.generatingTranslation || "AI 번역 생성 중";
        container.innerHTML = `<div class="c-report-loading u-p-8"><div class="spinner"></div><p class="u-mt-4">${msg}</p></div>`;
    },

    renderError(container: HTMLElement, message: string): void {
        const i18n = this.getI18n() as any;
        const retryMsg = i18n.retryLanguageSelection || "다시 한 번 언어를 선택해 주세요";
        container.innerHTML = `
            <div class="c-report-error u-p-8 u-text-error">
                ${message}
                <div class="u-mt-2 u-text-dim u-text-xs">${retryMsg}</div>
            </div>`;
    }
};
