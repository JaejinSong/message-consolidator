import { describe, it, expect, beforeEach } from 'vitest';
import { insightsRenderer } from './insightsRenderer.js';

describe('insightsRenderer.js', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="dailyGlance"></div>
            <div id="achievementsList"></div>
            <div id="sourceDistribution"></div>
        `;
    });

    it('should render daily glance correctly', () => {
        const data = { total_completed: 42, peak_time: '14:00', abandoned_count: 0 };
        insightsRenderer.renderDailyGlance(data, 'ko');
        
        const glance = document.getElementById('dailyGlance');
        expect(glance.textContent).toContain('42');
        expect(glance.textContent).toContain('14:00');
        expect(glance.textContent).toContain('완벽합니다');
    });

    it('should show warning in glance when abandoned tasks exist', () => {
        const data = { total_completed: 10, peak_time: '10:00', abandoned_tasks: 5 };
        insightsRenderer.renderDailyGlance(data, 'ko');
        expect(document.getElementById('dailyGlance').textContent).toContain('방치되었습니다');
    });

    it('should render achievements with locked/unlocked states', () => {
        const all = [
            { id: 'a1', name: 'Morning Star', desc: 'Desc' },
            { id: 'a2', name: 'Task Master', desc: 'Desc' }
        ];
        const user = [{ achievement_id: 'a1' }];
        
        insightsRenderer.renderAchievements(all, user, {});
        const list = document.getElementById('achievementsList');
        const items = list.querySelectorAll('.achievement-card');
        
        expect(items.length).toBe(2);
        expect(items[0].classList.contains('locked')).toBe(false);
        expect(items[1].classList.contains('locked')).toBe(true);
    });

    it('should render source distribution chart items', () => {
        const dist = { slack: 50, whatsapp: 50 };
        insightsRenderer.renderSourceDistribution({ source_distribution: dist });
        const chart = document.getElementById('sourceDistribution');
        expect(chart.querySelectorAll('.stacked-bar-segment').length).toBe(2);
        expect(chart.innerHTML).toContain('50%');
    });
});
