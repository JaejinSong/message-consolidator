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
        const valueSlot = document.getElementById('aiConsumptionValue');
        const detailSlot = document.getElementById('aiConsumptionDetail');
        if (!valueSlot || !detailSlot || !usage) return;

        valueSlot.textContent = String(usage.todayTotal);
        detailSlot.textContent = usage.model || "Preparing AI data...";
    },

    /**
     * Updates daily performance slots.
     * Target: #dailyGlanceValue, #dailyGlanceDetail
     */
    renderDailyGlance(stats: UserStats): void {
        const valueSlot = document.getElementById('dailyGlanceValue');
        const detailSlot = document.getElementById('dailyGlanceDetail');
        if (!valueSlot || !detailSlot) return;

        const abandoned = stats.abandoned_tasks || 0;
        const status = abandoned > 0 ? `⚠️ ${abandoned} Abandoned` : 'All tasks on track';

        valueSlot.textContent = String(stats.total_completed || 0);
        detailSlot.textContent = status;
    },

    /**
     * Updates achievement slot.
     * Target: #achievementsList
     */
    renderAchievements(all: any[], user: any[], _stats: any): void {
        const container = document.getElementById('achievementsList');
        if (!container) return;

        const unlockedIds = (user || []).map((u: any) => u.achievement_id || u.id);
        const seriesItems = (all || []).filter(a => a.name.includes('태스크 마스터') || a.name.includes('첫 걸음'));
        const nonSeriesItems = (all || []).filter(a => !a.name.includes('태스크 마스터') && !a.name.includes('첫 걸음'));

        const itemsToDisplay: any[] = [];
        if (seriesItems.length > 0) {
            const firstLocked = seriesItems.find(s => !unlockedIds.includes(s.id));
            itemsToDisplay.push(firstLocked || seriesItems[seriesItems.length - 1]);
        }
        itemsToDisplay.push(...nonSeriesItems);

        // Clear only the list container, not the whole card
        container.innerHTML = itemsToDisplay.slice(0, 3).map(ach => `
            <div class="c-achievement u-mb-1">${ach.icon || '🏆'} ${ach.name}</div>
        `).join('');
    },

    /**
     * Updates channel distribution slot.
     * Target: #sourceDistribution
     */
    renderChannelDistribution(stats: any): void {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;
        const dist = stats.source_distribution || {};
        const total = Object.values(dist).reduce((a: any, b: any) => (a as number) + (b as number), 0) as number;
        
        let html = '<div class="c-stacked-bar u-mt-4">';
        Object.entries(dist).forEach(([k, v]: [string, any]) => {
            const pct = Math.round(total > 0 ? (v / total) * 100 : 0);
            html += `<div class="c-stacked-bar__segment" style="width:${pct}%; background: var(--accent-light);" title="${k}: ${pct}%"></div>`;
        });
        html += '</div>';
        container.innerHTML = html;
    },

    renderHourlyActivity(stats: any): void {
        const valueSlot = document.getElementById('hourlyActivityValue');
        if (!valueSlot) return;
        valueSlot.textContent = stats.peak_time || "-";
    },

    renderActivityHeatmap(_stats: any): void {
        const container = document.getElementById('activityHeatmap');
        if (!container) return;
        container.innerHTML = `<div class="u-text-dim u-text-xs u-mt-4">Updating heatmap...</div>`;
    },

    renderAnkiChart(stats: any, days: number): void {
        const container = document.getElementById('ankiChartContainer');
        if (!container || !stats.completion_history) return;
        
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
