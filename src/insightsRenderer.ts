/**
 * src/insightsRenderer.ts
 * Refactored to use slot-based partial updates instead of innerHTML overwrites. 
 * Preserves the layout and BEM structure defined in index.html.
 */

import { state } from './state.ts';
import { I18N_DATA } from './locales.js';
import { UserStats, TokenUsage, IReportData, IReportNode, IReportLink } from './types.ts';
import * as echarts from 'echarts';
import { marked } from 'marked';
import { generateHeatmapData } from './logic.ts';

const chartInstances = new Map<string, echarts.ECharts>();

export const insightsRenderer = {
    getI18n() {
        const lang = state.currentLang || 'ko';
        return I18N_DATA[lang as keyof typeof I18N_DATA] || I18N_DATA.ko;
    },

    resizeAll() {
        chartInstances.forEach(chart => chart.resize());
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
     * Updates channel distribution slot.
     * Target: #sourceDistribution
     */
    renderChannelDistribution(stats: any): void {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;
        container.innerHTML = '';
        const dist = stats.source_distribution || {};
        const total = Object.values(dist).reduce((a: any, b: any) => (a as number) + (b as number), 0) as number;
        
        let html = '<div class="u-w-full"><div class="c-stacked-bar u-mt-4">';
        Object.entries(dist).forEach(([k, v]: [string, any]) => {
            const pct = Math.round(total > 0 ? (v / total) * 100 : 0);
            html += `<div class="c-stacked-bar__segment" style="width:${pct}%; background: var(--accent-light);" title="${k}: ${pct}%"></div>`;
        });
        html += '</div></div>';
        html += '<span class="stat-card__label u-mt-4">소스별 비중</span>';
        container.innerHTML = html;
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
        const cells = heatmapData.map(d => {
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
        t.style.left = `${e.pageX + 15}px`;
        t.style.top = `${e.pageY + 15}px`;
        t.classList.add('c-insights-tooltip--active');
    },

    renderAnkiChart(stats: any, days: number): void {
        const container = document.getElementById('ankiChartContainer');
        if (!container || !stats.completion_history) return;
        container.innerHTML = '';
        
        if (chartInstances.has('anki')) chartInstances.get('anki')?.dispose();
        const chart = echarts.init(container);
        chartInstances.set('anki', chart);

        const data = (stats.completion_history || []).slice(-days);
        chart.setOption({
            backgroundColor: 'transparent',
            xAxis: { type: 'category', data: data.map((d: any) => d.date) },
            yAxis: { type: 'value' },
            series: [{ type: 'line', data: data.map((d: any) => d.count), smooth: true, itemStyle: { color: 'var(--accent-color)' } }]
        });
    },

    // Report rendering remains largely the same but ensures container mapping
    renderReport(report: IReportData | null): void {
        const content = document.getElementById('reportSummaryContent');
        const viz = document.getElementById('reportVizChart');
        if (!content || !viz) return;
        content.innerHTML = '';
        viz.innerHTML = '';

        if (!report) {
            content.innerHTML = '<div class="u-text-dim">No historical reports found.</div>';
            viz.innerHTML = '';
            return;
        }

        const lang = state.currentLang || 'ko';
        const summary = report.translations?.[lang] || report.report_summary || report.summary || '';
        content.innerHTML = summary ? marked.parse(summary) as string : '<p>Summary pending...</p>';

        let vizData: IReportData['visualization_data'] | undefined;
        if (typeof report.visualization_data === 'string') {
            try { vizData = JSON.parse(report.visualization_data); } catch (e) { vizData = undefined; }
        } else {
            vizData = report.visualization_data;
        }

        if (vizData && (vizData as any).nodes && (vizData as any).links) {
            this.renderReportVisuals(vizData, viz);
        } else {
            viz.innerHTML = '<div class="u-text-dim">No visualization data available.</div>';
        }
    },

    renderReportVisuals(data: any, _container: HTMLElement): void {
        const netChart = document.getElementById('reportNetworkChart');
        const sankeyChart = document.getElementById('reportSankeyChart');
        if (!netChart || !sankeyChart) return;

        this.renderNetworkGraph(netChart, data);
        this.renderSankeyChart(sankeyChart, data);
        setTimeout(() => this.resizeAll(), 100);
    },

    renderNetworkGraph(container: HTMLElement, data: any): void {
        if (chartInstances.has('network')) chartInstances.get('network')?.dispose();
        const chart = echarts.init(container);
        chartInstances.set('network', chart);
        const nodes = (data.nodes || []).map((n: IReportNode) => ({ ...n, name: n.name || n.id }));
        chart.setOption({
            backgroundColor: 'transparent',
            series: [{ 
                type: 'graph', layout: 'force', data: nodes, 
                links: (data.links || []).filter((l: IReportLink) => nodes.some((n: IReportNode) => n.id === l.source) && nodes.some((n: IReportNode) => n.id === l.target)),
                label: { show: true, color: 'var(--text-main)' },
                force: { repulsion: 300 }
            }]
        });
    },

    renderSankeyChart(container: HTMLElement, data: any): void {
        if (chartInstances.has('sankey')) chartInstances.get('sankey')?.dispose();
        const chart = echarts.init(container);
        chartInstances.set('sankey', chart);
        const nodes = (data.nodes || []).map((n: IReportNode) => ({
            id: n.id, name: n.id, alias: n.name,
            itemStyle: { color: n.is_me ? 'var(--color-error)' : 'var(--accent-color)' }
        }));
        chart.setOption({
            backgroundColor: 'transparent',
            series: [{ 
                type: 'sankey', layout: 'none', data: nodes, 
                links: this.mergeSankeyLinks(data.links || []),
                lineStyle: { color: 'gradient', curveness: 0.5 }
            }]
        });
    },

    mergeSankeyLinks(links: any[]): any[] {
        const merged: any[] = [];
        (links || []).forEach(l => {
            if (l.source === l.target) return;
            const existing = merged.find(m => (m.source === l.source && m.target === l.target) || (m.source === l.target && m.target === l.source));
            if (existing) existing.value += l.value;
            else merged.push({ ...l });
        });
        return merged;
    },

    renderReportList(reports: IReportData[], activeId: number | null): void {
        const container = document.getElementById('reportList');
        if (!container) return;
        container.innerHTML = '';
        container.innerHTML = (reports || []).map(r => `
            <div class="c-insights-report-item ${r.id === activeId ? 'c-insights-report-item--active' : ''}" data-id="${r.id}">
                <div class="u-flex u-flex-between u-flex-align-center u-w-full">
                    <div class="u-flex-column">
                         <span class="c-insights-report-item__date">${r.start_date || ''}</span>
                         <span class="c-insights-report-item__title">${r.title || 'Weekly Analysis'}</span>
                    </div>
                </div>
            </div>
        `).join('');
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
