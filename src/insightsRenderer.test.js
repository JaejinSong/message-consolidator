import { describe, it, expect, beforeEach, vi } from 'vitest';
import { insightsRenderer } from './insightsRenderer.ts';
import { state } from './state.ts';

describe('insightsRenderer.ts - Slot-based Rendering (JS Test)', () => {
    const mockI18n = {
        completedTasks: '완료 업무',
        totalCommunication: '총 소통량',
        waiting: '대기',
        tokenUsageTitle: '토큰 사용량',
        sourceDistribution: '소스별 비중',
        weeklyReportTitle: '주간 리포트'
    };

    beforeEach(() => {
        state.currentLang = 'ko';
        // HTML slots must match index.html exactly for valid DOM interaction
        document.body.innerHTML = `
            <div class="c-insights-card" id="cardDailyGlance">
                <h3 class="c-insights-card__title">Daily Stats</h3>
                <div id="dailyGlanceValue" class="c-insights-card__main-value">-</div>
                <div id="dailyGlanceDetail" class="c-insights-card__detail">Syncing...</div>
            </div>
            <div class="c-insights-card c-insights-card--square" id="ai-usage-consolidated">
                <div class="u-text-dim u-text-sm">AI Usage Data Syncing...</div>
            </div>
            <div class="c-insights-card" id="cardAchievements">
                <h3 class="c-insights-card__title">Achievements</h3>
                <div id="achievementsList" class="c-insights-achievements-list"></div>
            </div>
            <div id="sourceDistribution"></div>
            <div id="hourlyActivityValue"></div>
            <div id="ankiChartContainer" style="width:100px; height:100px;"></div>
        `;
    });

    it('should update daily glance slots without destroying card title', () => {
        const data = { total_completed: 42, peak_time: '14:00' };
        insightsRenderer.renderDailyGlance(data, mockI18n);

        const card = document.getElementById('cardDailyGlance');
        const value = document.getElementById('dailyGlanceValue');
        const title = card.querySelector('.c-insights-card__title');

        expect(value.textContent).toContain('42');
        expect(title.textContent).toBe('Daily Stats');
        expect(card.classList.contains('c-insights-card')).toBe(true);
    });

    it('should update consolidated AI usage widget with formatted numbers and breakdown (Daily/Monthly)', () => {
        const usage = { 
            todayTotal: 1234, todayPrompt: 600, todayCompletion: 634,
            monthlyTotal: 56789, monthlyPrompt: 30000, monthlyCompletion: 26789,
            monthlyCost: 1.25, model: 'Gemini 3 Flash' 
        };
        insightsRenderer.renderTokenUsage(usage, mockI18n);
        
        const slot = document.getElementById('ai-usage-consolidated');
        expect(slot.innerHTML).toContain('토큰 사용량');
        expect(slot.innerHTML).toContain('1,234');
        expect(slot.innerHTML).toContain('입 600');
        expect(slot.innerHTML).toContain('출 634');
        expect(slot.innerHTML).toContain('56,789');
        expect(slot.innerHTML).toContain('입 30,000');
        expect(slot.innerHTML).toContain('출 26,789');
        expect(slot.innerHTML).toContain('$1.25');
        expect(slot.innerHTML).toContain('Gemini 3 Flash');
    });

    it('should render achievements into the list slot', () => {
        const all = [{ id: '1', name: 'Achievement 1', icon: '🏆' }];
        insightsRenderer.renderAchievements(all, [], {});

        const list = document.getElementById('achievementsList');
        expect(list.innerHTML).toContain('Achievement 1');
    });

    it('should render source distribution chart container and labels correctly', () => {
        const stats = { source_distribution: { slack: 70, whatsapp: 30 } };
        insightsRenderer.renderChannelDistribution(stats, mockI18n);

        const container = document.getElementById('sourceDistribution');
        const chartNode = document.getElementById('sourceDistributionChart');
        
        expect(chartNode).not.toBeNull();
        expect(container.innerHTML).toContain('소스별 비중');
    });
});
