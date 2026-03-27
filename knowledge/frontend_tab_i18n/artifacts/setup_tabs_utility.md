# SetupTabs Utility Architecture

## Context
Previously, `setupTabs` was a private function in `app.js`. It had a hardcoded selector check to decide whether to trigger `fetchMessages()`, which led to a mismatch when CSS classes changed.

## Refactored Utility (`utils.js`)
- **Location**: `static/js/utils.js` (exported)
- **Design Pattern**: Callback-driven (`onSwitch` hook).
- **Functionality**:
    - Manages class toggling for both buttons and content panels.
    - Supports BEM-style active modifiers (e.g., `c-settings__panel--active`).
    - Executes an optional callback on every tab switch to allow context-specific actions (like refreshing the dashboard).

## Usage Example
```javascript
import { setupTabs } from './js/utils.js';

// Dashboard context
setupTabs('.tab-btn', '.tab-panel', 'data-tab', 'active', (tabId) => {
    fetchMessages(); // Triggers refresh
});

// Settings context
setupTabs('.c-settings__tab', '.c-settings__panel', 'data-settings-tab', 'c-settings__tab--active');
```

## Testing Protocol
- Verified in `utils.test.js` for:
    - Default/Custom active class application.
    - Callback execution with correct target ID.
    - BEM modifier support.
