# Verification Scenarios

## 🎨 UI & Layout
### Card Restructuring (PC View)
- [ ] Task card uses flattened column structure (`.col-source`, `.col-task`, `.col-room`, etc.).
- [ ] Columns align correctly with the table header.
- [ ] No overflow in "SOURCE" column on narrow PC views.

### SVG Icons
- [ ] Source icons (Slack, WhatsApp, Gmail) use SVG instead of emojis.
- [ ] Action buttons (Link, Done, Delete) use consistent `.action-btn` class and SVG icons.
- [ ] Delete button uses the trash can icon.

## ⚖️ Business Logic & Badges
### Task Aging Markers
- [ ] **Stale (24h+)**: Displayed with a clock icon and "Stale" (정체됨) text.
- [ ] **Abandoned (72h+)**: Displayed with an alert icon and "Abandoned" (방치됨) text.
- [ ] Logic uses `diffHours >= 24` and `diffHours >= 72`.

### Internationalization (i18n)
- [ ] Badges reflect the selected language (KO, EN, ID, TH).
- [ ] Empty state messages rotate or display correctly as defined in `locales.js`.

## 🧪 Automated Tests (Vitest)
- [ ] `make test-ui` (Runs all Vitest + Happy DOM tests)
- [ ] `static/js/logic.test.js` (Core logic validation)
- [ ] `static/js/renderer.test.js` (DOM/HTML structure validation)
- [ ] `static/js/utils.test.js` (Utility functions & DOM interaction)
