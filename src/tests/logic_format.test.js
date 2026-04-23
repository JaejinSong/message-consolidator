import {
    getDeadlineBadge,
    parseMarkdown
} from '../logic';
import { describe, it, expect, vi, afterEach } from 'vitest';
vi.unmock('marked');

describe('logic.js - getDeadlineBadge', () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it('should return empty string if task is done', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
        expect(getDeadlineBadge('2026-04-23', true)).toBe('');
    });

    it('should return today badge when deadline is today', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
        const badge = getDeadlineBadge('2026-04-23', false, 'ko');
        expect(badge).toContain('c-badge--deadline-today');
        expect(badge).toContain('오늘');
    });

    it('should return tomorrow badge when deadline is tomorrow', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
        const badge = getDeadlineBadge('2026-04-24', false, 'ko');
        expect(badge).toContain('c-badge--deadline-tomorrow');
        expect(badge).toContain('내일');
    });

    it('should return past badge for recently missed deadline', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-04-23T10:00:00Z'));
        const badge = getDeadlineBadge('2026-04-21', false, 'ko');
        expect(badge).toContain('c-badge--deadline-past');
        expect(badge).toContain('지남');
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
        const html = parseMarkdown('- item');
        expect(html).toContain('<ul');
        expect(html).toContain('<li');
        expect(html).toContain('item');
    });
});
