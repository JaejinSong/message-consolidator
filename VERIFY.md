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

## 🧪 Automated Scripts
- [ ] `node static/js/verify_logic.js` (Core logic validation)
- [ ] `node static/js/verify_renderer.js` (DOM/HTML structure validation)
