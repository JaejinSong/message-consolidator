import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';

describe('Mobile Layout Regression Tests', () => {
    const baseCssPath = path.resolve(process.cwd(), 'static/css/base.css');
    const layoutCssPath = path.resolve(process.cwd(), 'static/css/layout.css');

    it('base.css should have reduced body padding for mobile (480px)', () => {
        const content = fs.readFileSync(baseCssPath, 'utf8');
        // CSS custom properties are not supported as length values inside @media queries
        // in any major browser as of 2026 — must use a literal value (30rem == --bp-mobile).
        const hasMobilePadding = /@media\s*\(max-width:\s*30rem\)\s*{\s*body\s*{\s*padding:\s*0\.5rem;/.test(content);
        expect(hasMobilePadding).toBe(true);
    });

    it('base.css must not use CSS custom properties as @media length values', () => {
        const content = fs.readFileSync(baseCssPath, 'utf8');
        // Regression: @media (max-width: var(--bp-*)) silently never matches.
        const usesCustomPropInMedia = /@media\s*\([^)]*var\(--bp-/.test(content);
        expect(usesCustomPropInMedia).toBe(false);
    });

    it('message-card.css should drop min-width on mobile to prevent horizontal overflow', () => {
        const cardCssPath = path.resolve(process.cwd(), 'static/css/components/message-card.css');
        const content = fs.readFileSync(cardCssPath, 'utf8');
        // Regression: .c-message-card { min-width: 25rem } overflowed iPhone-class viewports.
        const hasMobileOverride = /@media\s*\(max-width:\s*48rem\)\s*{\s*\.c-message-card\s*{\s*min-width:\s*0;/.test(content);
        expect(hasMobileOverride).toBe(true);
    });

    it('layout.css should have optimized glass-container padding for mobile breakpoints', () => {
        const content = fs.readFileSync(layoutCssPath, 'utf8');
        // Check for padding: 1.25rem 0.75rem in the mobile media query
        const hasOptimizedPadding = /padding:\s*1\.25rem\s*0\.75rem;/.test(content);
        const hasOptimizedRadius = /border-radius:\s*var\(--radius-lg\);/.test(content);

        expect(hasOptimizedPadding).toBe(true);
        expect(hasOptimizedRadius).toBe(true);
    });

    it('no hardcoded pixel values should exist in layout.css', () => {
        const content = fs.readFileSync(layoutCssPath, 'utf8');
        // @media 선언부와 주석을 제외하여 오인을 방지합니다.
        const lines = content.split('\n');
        const propertyLines = lines.filter((line: string) => !line.trim().startsWith('@media'));
        const cleanContent = propertyLines.join('\n').replace(/\/\*[\s\S]*?\*\//g, '');

        const hasHardcodedPx = /(?<!var\(--)[0-9]+px/.test(cleanContent);
        expect(hasHardcodedPx).toBe(false);
    });
});
