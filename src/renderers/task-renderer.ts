import { ICONS } from '../icons';

/**
 * @file task-renderer.ts
 * @description Logic for rendering multi-channel source badges with deduplication and a11y.
 */

/**
 * Renders a list of source channel icons with deduplication and accessibility.
 * @param channels Array of source channels (e.g. ['slack', 'whatsapp'])
 * @param fallback Fallback source if channels array is empty
 */
export function renderSourceList(channels?: string[], fallback?: string): string {
    const rawList = channels && channels.length > 0 ? channels : [fallback || 'gmail'];
    const uniqueChannels = Array.from(new Set(rawList));

    const badges = uniqueChannels.map(channel => {
        const icon = ICONS[channel as keyof typeof ICONS] || ICONS.gmail;
        const label = getChannelLabel(channel);
        
        return `
            <div class="task-card__source-icon task-card__source-icon--${channel.toLowerCase()}" 
                 title="${label}" 
                 aria-label="${label}">
                ${icon}
            </div>
        `;
    }).join('');

    return `<div class="task-card__source-list">${badges}</div>`;
}

/**
 * Returns human-readable channel label for accessibility.
 */
function getChannelLabel(channel: string): string {
    const labels: Record<string, string> = {
        slack: 'Slack',
        whatsapp: 'WhatsApp',
        gmail: 'Gmail'
    };
    return labels[channel.toLowerCase()] || 'Other Source';
}
