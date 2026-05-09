import { marked } from 'marked';
import type { MarkedExtension, TokenizerAndRendererExtension } from 'marked';

type CalloutType = 'note' | 'tip' | 'important' | 'warning' | 'caution';

const CALLOUT_ICONS: Record<CalloutType, string> = {
    note:      'ℹ️',
    tip:       '💡',
    important: '❗',
    warning:   '⚠️',
    caution:   '🛑',
};

const CALLOUT_PATTERN = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]/i;
// Matches a full blockquote block: one or more lines starting with >
const BLOCK_START = /^(?:> ?.*(?:\n|$))+/;

const calloutTokenizer: TokenizerAndRendererExtension = {
    name: 'callout',
    level: 'block',

    start(src: string): number | void {
        const idx = src.indexOf('>');
        return idx >= 0 ? idx : undefined;
    },

    tokenizer(src: string) {
        const blockMatch = BLOCK_START.exec(src);
        if (!blockMatch) return undefined;

        const raw = blockMatch[0];
        const lines = raw.split('\n').map(l => l.replace(/^>\s?/, ''));
        const firstLine = lines[0].trimStart();
        const typeMatch = CALLOUT_PATTERN.exec(firstLine);
        if (!typeMatch) return undefined;

        const type = typeMatch[1].toLowerCase() as CalloutType;
        const body = lines.slice(1).join('\n').trim();

        return {
            type: 'callout',
            raw,
            calloutType: type,
            body,
        };
    },

    renderer(token) {
        const t = token as unknown as { calloutType: CalloutType; body: string };
        const icon = CALLOUT_ICONS[t.calloutType] ?? '';
        const renderedBody = marked.parse(t.body) as string;
        return (
            `<div class="c-guide__callout c-guide__callout--${t.calloutType}" role="note">` +
            `<div class="c-guide__callout-icon" aria-hidden="true">${icon}</div>` +
            `<div class="c-guide__callout-body">${renderedBody}</div>` +
            `</div>`
        );
    },
};

export const calloutExtension: MarkedExtension = {
    extensions: [calloutTokenizer],
};
