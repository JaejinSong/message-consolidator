import {
    getDeadlineBadge,
    parseMarkdown
} from '../logic.js';
import { describe, it, expect, vi, afterEach } from 'vitest';

describe('logic.js - getDeadlineBadge', () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it('should return empty string if task is done', () => {
        expect(getDeadlineBadge(new Date().toISOString(), true)).toBe('');
    });

    it('should return stale badge after 24 hours', () => {
        // Wednesday at noon
        const now = new Date('2026-03-25T12:00:00Z');
        vi.useFakeTimers();
        vi.setSystemTime(now);

        // Created Tuesday morning (non-weekend transition)
        const staleDate = new Date(now.getTime() - 26 * 60 * 60 * 1000).toISOString();
        const badge = getDeadlineBadge(staleDate, false, 'ko');
        expect(badge).toContain('c-badge--priority-medium');
        expect(badge).toContain('정체됨');
    });

    it('should return abandoned badge after 72 hours (excluding weekends)', () => {
        // Thursday to Wednesday next week (enough time even with weekends)
        const past = new Date('2026-03-12T10:00:00Z').toISOString();
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-03-18T10:00:00Z')); // 6 days later (including Sat/Sun)
        const badge = getDeadlineBadge(past, false, 'ko');
        expect(badge).toContain('c-badge--priority-high');
        expect(badge).toContain('방치됨');
    });

    it('should ignore weekends in calculation', () => {
        // Friday to Monday 
        const fri = '2026-03-20T12:00:00Z';
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-03-23T13:00:00Z')); // ~73 hours elapsed
        const badge = getDeadlineBadge(fri, false, 'ko');
        // 73 hours - 48 hours (weekend) = 25 hours -> Stale
        expect(badge).toContain('c-badge--priority-medium');
        expect(badge).not.toContain('c-badge--priority-high');
    });
});

describe('logic.js - parseMarkdown', () => {
    it('should parse headers correctly', () => {
        expect(parseMarkdown('# Hello')).toContain('<h1');
        expect(parseMarkdown('## World')).toContain('<h2');
        expect(parseMarkdown('### Sub')).toContain('<h3');
    });

    it('should parse bold and code', () => {
        expect(parseMarkdown('**bold**')).toContain('<strong');
        expect(parseMarkdown('**bold**')).toContain('bold</strong>');
        expect(parseMarkdown('`code`')).toContain('<code');
        expect(parseMarkdown('`code`')).toContain('code</code>');
    });

    it('should parse lists', () => {
        expect(parseMarkdown('- item')).toContain('•');
        expect(parseMarkdown('- item')).toContain('item</div>');
    });
});
