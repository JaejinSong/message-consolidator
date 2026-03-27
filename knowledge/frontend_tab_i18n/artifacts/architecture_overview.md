# I18n DOM Stability and Tab Architecture

## Problem: DOM Corruption via I18n
Original `updateCountBadge` in `i18n.js` was using `innerHTML` to update category tab counters. This destroyed nested `<span>` elements (used for translations and badges) and lost event listeners if they were attached directly to child elements.

## Solution: Surgical DOM Updates
- **`i18n.js` Refinement**: Switched to targeted selector-based updates. Instead of replacing the entire button content, it now updates specific `<span>` children via `textContent`.
- **Structural Enforcement**: Tabs in `index.html` were standardized to use nested `<span>` with `data-i18n` attributes to prevent text content from being mixed with badge elements.

## Regression Prevention
- **Multi-toggle Test**: A dedicated test in `i18n.test.js` verifies that repeated language switches (EN -> KO -> EN) do NOT corrupt the nested DOM structure.
