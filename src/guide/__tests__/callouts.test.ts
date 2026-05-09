// Why: real marked needed — override global setup mock for this file only
import { describe, it, expect, vi, beforeAll } from 'vitest';

vi.mock('marked', async () => {
    const actual = await vi.importActual<typeof import('marked')>('marked');
    return actual;
});

import { marked } from 'marked';
import { calloutExtension } from '../callouts';

beforeAll(() => {
    marked.use(calloutExtension);
});

describe('calloutExtension', () => {
    it('renders [!NOTE] blockquote as callout with correct classes', () => {
        const input = '> [!NOTE]\n> This is a note body.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout');
        expect(html).toContain('c-guide__callout--note');
        expect(html).toContain('role="note"');
        expect(html).toContain('c-guide__callout-icon');
        expect(html).toContain('c-guide__callout-body');
    });

    it('renders [!NOTE] with correct icon', () => {
        const input = '> [!NOTE]\n> body';
        const html = marked.parse(input) as string;
        expect(html).toContain('ℹ️');
    });

    it('renders [!TIP] with correct icon and class', () => {
        const input = '> [!TIP]\n> A helpful tip.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout--tip');
        expect(html).toContain('💡');
    });

    it('renders [!IMPORTANT] with correct icon and class', () => {
        const input = '> [!IMPORTANT]\n> Very important.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout--important');
        expect(html).toContain('❗');
    });

    it('renders [!WARNING] with correct icon and class', () => {
        const input = '> [!WARNING]\n> Watch out.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout--warning');
        expect(html).toContain('⚠️');
    });

    it('renders [!CAUTION] with correct icon and class', () => {
        const input = '> [!CAUTION]\n> Be careful.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout--caution');
        expect(html).toContain('🛑');
    });

    it('is case-insensitive: [!warning] produces --warning', () => {
        const input = '> [!warning]\n> lowercase type.';
        const html = marked.parse(input) as string;
        expect(html).toContain('c-guide__callout--warning');
    });

    it('leaves regular blockquotes unaffected', () => {
        const input = '> This is a plain blockquote without a type marker.';
        const html = marked.parse(input) as string;
        expect(html).not.toContain('c-guide__callout');
        expect(html).toContain('<blockquote>');
    });

    it('does not strip body from regular blockquote', () => {
        const input = '> Some regular content here.';
        const html = marked.parse(input) as string;
        expect(html).toContain('Some regular content here.');
    });

    it('renders body as markdown (inline formatting preserved)', () => {
        const input = '> [!NOTE]\n> **bold text** and _italic_.';
        const html = marked.parse(input) as string;
        expect(html).toContain('<strong>bold text</strong>');
    });
});
