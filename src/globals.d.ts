// Why: `window.insights` is set by insights.init() so HTML-bound renderer callbacks (history
// list click) can reach back into the controller without an import cycle. Keep this file flat —
// only declare globals here, no runtime exports.

import type { insights } from './insights';

declare global {
    interface Window {
        insights?: typeof insights;
    }
}

export {};
