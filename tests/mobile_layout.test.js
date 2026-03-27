import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';

describe('Mobile Layout Regression Tests', () => {
    const baseCssPath = path.resolve(process.cwd(), 'static/css/base.css');
    const layoutCssPath = path.resolve(process.cwd(), 'static/css/layout.css');

    it('base.css should have reduced body padding for mobile (480px)', () => {
        const content = fs.readFileSync(baseCssPath, 'utf8');
        // Check for media query and body padding
        const hasMobilePadding = /@media\s*\(max-width:\s*480px\)\s*{\s*body\s*{\s*padding:\s*0\.5rem;/.test(content);
        expect(hasMobilePadding).toBe(true);
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
        const propertyLines = lines.filter(line => !line.trim().startsWith('@media'));
        const cleanContent = propertyLines.join('\n').replace(/\/\*[\s\S]*?\*\//g, '');
        
        const hasHardcodedPx = /(?<!var\(--)[0-9]+px/.test(cleanContent);
        expect(hasHardcodedPx).toBe(false);
    });
});
