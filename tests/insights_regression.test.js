import { describe, it, expect, beforeAll } from 'vitest';
import { Window } from 'happy-dom';
import fs from 'fs';
import path from 'path';

/**
 * @file insights_regression.test.js
 * @description Regression tests to ensure Insights dashboard layout and logic integrity.
 */

describe('Insights Dashboard - Layout & Logic Regression', () => {
    let document;
    let htmlPath = path.resolve(process.cwd(), 'index.html');

    beforeAll(() => {
        const html = fs.readFileSync(htmlPath, 'utf8');
        const window = new Window();
        document = window.document;
        document.write(html);
    });

    it('should have a standardized grid container for Insights', () => {
        const dashboard = document.querySelector('.c-insights-dashboard');
        expect(dashboard, 'Insights grid container should exist').not.toBeNull();
    });

    it('should contain all required section titles via i18n attributes', () => {
        const expectedI18nKeys = ['waitingTasks', 'reviewStatsTitle'];
        const headers = Array.from(document.querySelectorAll('.c-insights-section-title'))
            .map(h => h.getAttribute('data-i18n'));
        
        expectedI18nKeys.forEach(key => {
            expect(headers).toContain(key);
        });
    });

    it('should apply .c-insights-card to all grid tiles', () => {
        const gridTileIds = [
            'stat-completed', 'stat-peak', 'stat-stale',
            'ai-consumption-today', 'ai-consumption-monthly', 'ai-consumption-cost',
            'achievement-slot-1', 'achievement-slot-2', 'achievement-slot-3'
        ];
        
        gridTileIds.forEach(id => {
            const card = document.getElementById(id);
            expect(card, `Card #${id} should exist`).not.toBeNull();
            expect(card.classList.contains('c-insights-card'), `Element #${id} must be a card`).toBe(true);
        });
    });

    it('should have .c-insights-card--full for the trends chart', () => {
        const trendCard = document.querySelector('.c-insights-card--full');
        expect(trendCard, 'Trend chart card should be full width').not.toBeNull();
    });

    it('should have critical CSS rules in v2-insights.css', () => {
        const cssPath = path.resolve(process.cwd(), 'static/css/v2-insights.css');
        const content = fs.readFileSync(cssPath, 'utf8');
        expect(content).toContain('aspect-ratio: 1');
        expect(content).toContain('grid-template-columns: repeat(3, 1fr)');
    });
});
