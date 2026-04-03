import { describe, it, expect } from 'vitest';
import {
    calculateHeatmapLevel,
    processTimeSeriesData
} from '../logic.js';

describe('logic.js - calculateHeatmapLevel', () => {
    it('should return correct level based on task count', () => {
        expect(calculateHeatmapLevel(0)).toBe(0);
        expect(calculateHeatmapLevel(2)).toBe(1);
        expect(calculateHeatmapLevel(4)).toBe(2);
        expect(calculateHeatmapLevel(6)).toBe(3);
        expect(calculateHeatmapLevel(10)).toBe(4);
        expect(calculateHeatmapLevel(-5)).toBe(0);
    });
});

describe('logic.js - processTimeSeriesData', () => {
    it('should generate continuous daily data', () => {
        const today = new Date();
        today.setHours(0, 0, 0, 0);
        const yesterday = new Date(today);
        yesterday.setDate(yesterday.getDate() - 1);
        const yStr = yesterday.toISOString().split('T')[0];

        const rawHistory = [
            { date: yStr, counts: { slack: 5, telegram: 2 } }
        ];

        const processed = processTimeSeriesData(rawHistory, 3);
        expect(processed.length).toBe(3);
        expect(processed[1].cumulative).toBe(7);
    });
});
