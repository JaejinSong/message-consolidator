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
    let htmlPath = path.resolve(process.cwd(), 'static/index.html');

    beforeAll(() => {
        const html = fs.readFileSync(htmlPath, 'utf8');
        const window = new Window();
        document = window.document;
        document.write(html);
    });

    it('should have a standardized grid container for Insights', () => {
        const dashboard = document.querySelector('.insights-dashboard');
        expect(dashboard, 'Insights grid container should exist').not.toBeNull();
    });

    it('should contain all required section titles via i18n attributes', () => {
        const expectedI18nKeys = ['waitingTasks', 'heatmapTitle', 'achievementsTitle'];
        const headers = Array.from(document.querySelectorAll('.insights-section-title'))
            .map(h => h.getAttribute('data-i18n'));
        
        expectedI18nKeys.forEach(key => {
            expect(headers).toContain(key);
        });
    });

    it('should apply .insights-card--square to designated metric tiles', () => {
        const squareTileContentIds = [
            'waitingMetricsMe', 
            'waitingMetricsAttention', 
            'activityHeatmap', 
            'sourceDistribution', 
            'hourlyActivity'
        ];
        
        squareTileContentIds.forEach(id => {
            const contentEl = document.getElementById(id);
            expect(contentEl, `Element #${id} should exist`).not.toBeNull();
            
            // The card is the parent or ancestor with .insights-card
            const card = contentEl.closest('.insights-card');
            expect(card, `Parent card for #${id} should exist`).not.toBeNull();
            expect(card.classList.contains('insights-card--square'), `Card for #${id} must be square`).toBe(true);
        });
    });

    it('should have .insights-card class on all interactive grid items', () => {
        const cards = document.querySelectorAll('.insights-card');
        expect(cards.length, 'Should have multiple insight cards in the grid').toBeGreaterThan(5);
    });

    it('Charts and Achievements should be full-width (not square)', () => {
        const fullWidthIds = ['ankiChartContainer', 'achievementsList'];
        fullWidthIds.forEach(id => {
            const contentEl = document.getElementById(id);
            if (contentEl) {
                const card = contentEl.closest('.insights-card');
                expect(card.classList.contains('insights-card--square'), `${id} card should NOT be square`).toBe(false);
            }
        });
    });

    it('should have critical CSS rules for square tiles in insights.css', () => {
        const cssPath = path.resolve(process.cwd(), 'static/css/components/insights.css');
        const content = fs.readFileSync(cssPath, 'utf8');
        expect(content).toContain('aspect-ratio: 1 / 1');
        expect(content).toContain('min-width: 0 !important');
        expect(content).toContain('grid-template-columns: repeat(3, 1fr)');
    });
});
