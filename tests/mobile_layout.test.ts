import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';

describe('Mobile Layout Regression Tests', () => {
    const baseCssPath = path.resolve(process.cwd(), 'static/css/base.css');
    const layoutCssPath = path.resolve(process.cwd(), 'static/css/layout.css');

    it('base.css should remove body padding at 30rem to prevent double-padding with glass-container', () => {
        const content = fs.readFileSync(baseCssPath, 'utf8');
        // MOBILE-7: body padding: 0 at 30rem so glass-container fills edge-to-edge,
        // eliminating the ~2.5rem combined padding that crushed content on 320px viewports.
        const hasZeroPadding = /@media\s*\(max-width:\s*30rem\)[^}]*{\s*[^}]*body\s*{[^}]*padding:\s*0;/.test(content);
        expect(hasZeroPadding).toBe(true);
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

    it('base.css should set input font-size to 1rem on mobile to prevent iOS Safari zoom', () => {
        const content = fs.readFileSync(baseCssPath, 'utf8');
        // iOS Safari auto-zooms when focused input font-size < 16px (1rem).
        const hasInputFontFix = /@media\s*\(max-width:\s*48rem\)[\s\S]*?\.c-input[\s\S]*?font-size:\s*1rem/.test(content);
        expect(hasInputFontFix).toBe(true);
    });

    it('variables.css should define --touch-target-min and --btn-size-md tokens', () => {
        const varsCssPath = path.resolve(process.cwd(), 'static/css/variables.css');
        const content = fs.readFileSync(varsCssPath, 'utf8');
        expect(content).toMatch(/--touch-target-min:\s*2\.75rem/);
        expect(content).toMatch(/--btn-size-md:\s*2\.25rem/);
    });

    it('v2-insights.css should increase sidebar max-height at 64rem for better list visibility', () => {
        const insightsCssPath = path.resolve(process.cwd(), 'static/css/v2-insights.css');
        const content = fs.readFileSync(insightsCssPath, 'utf8');
        // Regression: 12.5rem (~200px) showed only 3-4 items at 1rem padding each.
        const hasBetterHeight = /@media\s*\(max-width:\s*64rem\)[\s\S]*?\.c-insights-report-sidebar[\s\S]*?max-height:\s*18rem/.test(content);
        expect(hasBetterHeight).toBe(true);
    });

    it('v2-insights.css report layout must use minmax(0,1fr) rows so main scrolls instead of blowing out', () => {
        const insightsCssPath = path.resolve(process.cwd(), 'static/css/v2-insights.css');
        const content = fs.readFileSync(insightsCssPath, 'utf8');
        // Regression: bare `grid-template-rows: 1fr` (=minmax(auto,1fr)) let the tall
        // sidebar report list expand the row past the fixed-height container, so
        // .c-insights-report-main overflowed `overflow:hidden` and its scrollbar was
        // unreachable. minmax(0,1fr) clamps the row to the container height.
        const usesMinmaxRows = /\.c-insights-report-layout\s*{[\s\S]*?grid-template-rows:\s*minmax\(\s*0\s*,\s*1fr\s*\)/.test(content);
        const usesBareFrRows = /grid-template-rows:\s*1fr\s*;/.test(content);
        expect(usesMinmaxRows).toBe(true);
        expect(usesBareFrRows).toBe(false);
    });

    it('message-card.css should stack footer to column on narrow mobile to prevent overflow', () => {
        const cardCssPath = path.resolve(process.cwd(), 'static/css/components/message-card.css');
        const content = fs.readFileSync(cardCssPath, 'utf8');
        const hasColumnFooter = /@media\s*\(max-width:\s*30rem\)[\s\S]*?\.c-message-card__footer[\s\S]*?flex-direction:\s*column/.test(content);
        expect(hasColumnFooter).toBe(true);
    });

    it('layout.css should remove glass-container side borders and radius at 30rem to fill edge-to-edge', () => {
        const content = fs.readFileSync(layoutCssPath, 'utf8');
        const hasEdgeToEdge = /@media\s*\(max-width:\s*30rem\)[\s\S]*?\.glass-container[\s\S]*?border-radius:\s*0/.test(content);
        expect(hasEdgeToEdge).toBe(true);
    });

    it('tabs.css should use horizontal-scroll pattern (not grid) on mobile', () => {
        const tabsCssPath = path.resolve(process.cwd(), 'static/css/components/tabs.css');
        const content = fs.readFileSync(tabsCssPath, 'utf8');
        // Regression: grid 2-col forced awkward layout with odd tab counts.
        const usesGrid = /@media\s*\(max-width:\s*(?:480px|30rem)\)[\s\S]*?display:\s*grid/.test(content);
        const hasHScroll = /@media\s*\(max-width:\s*30rem\)[\s\S]*?overflow-x:\s*auto/.test(content);
        expect(usesGrid).toBe(false);
        expect(hasHScroll).toBe(true);
    });

    it('v2-insights.css should reduce report table font-size and stack controls on mobile', () => {
        const insightsCssPath = path.resolve(process.cwd(), 'static/css/v2-insights.css');
        const content = fs.readFileSync(insightsCssPath, 'utf8');
        const hasTableFontReduce = /@media\s*\(max-width:\s*48rem\)[\s\S]*?\.c-report-table[\s\S]*?font-size:\s*0\.75rem/.test(content);
        const hasDateFullWidth = /@media\s*\(max-width:\s*48rem\)[\s\S]*?#reportStartDate[\s\S]*?width:\s*100%\s*!important/.test(content);
        expect(hasTableFontReduce).toBe(true);
        expect(hasDateFullWidth).toBe(true);
    });

    it('v2-modals.css should use bottom-sheet pattern with 100dvh on small mobile', () => {
        const modalsCssPath = path.resolve(process.cwd(), 'static/css/v2-modals.css');
        const content = fs.readFileSync(modalsCssPath, 'utf8');
        const hasBottomSheet = /@media\s*\(max-width:\s*30rem\)[\s\S]*?align-items:\s*flex-end/.test(content);
        const hasDvh = /@media\s*\(max-width:\s*30rem\)[\s\S]*?max-height:\s*100dvh/.test(content);
        expect(hasBottomSheet).toBe(true);
        expect(hasDvh).toBe(true);
    });

    it('v2-modals.css should enforce touch target min-size for .c-modal__close on mobile', () => {
        const modalsCssPath = path.resolve(process.cwd(), 'static/css/v2-modals.css');
        const content = fs.readFileSync(modalsCssPath, 'utf8');
        const hasTouchTarget = /@media\s*\(max-width:\s*48rem\)[\s\S]*?\.c-modal__close[\s\S]*?min-width:\s*var\(--touch-target-min\)/.test(content);
        expect(hasTouchTarget).toBe(true);
    });

    it('message-card.css should enforce touch target for action buttons on mobile', () => {
        const cardCssPath = path.resolve(process.cwd(), 'static/css/components/message-card.css');
        const content = fs.readFileSync(cardCssPath, 'utf8');
        const hasTouchTarget = /@media\s*\(max-width:\s*48rem\)[\s\S]*?\.c-message-card__action-btn[\s\S]*?min-width:\s*var\(--touch-target-min\)/.test(content);
        expect(hasTouchTarget).toBe(true);
    });

    it('archive-table.css should reduce cell padding on mobile (48rem)', () => {
        const archiveCssPath = path.resolve(process.cwd(), 'static/css/components/archive-table.css');
        const content = fs.readFileSync(archiveCssPath, 'utf8');
        const hasMobilePadding = /@media\s*\(max-width:\s*48rem\)[\s\S]*?\.c-archive-table\s+th[\s\S]*?padding:\s*0\.75rem\s*0\.5rem/.test(content);
        expect(hasMobilePadding).toBe(true);
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
