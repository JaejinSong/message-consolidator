// @vitest-environment happy-dom
import { describe, it, expect, beforeEach } from 'vitest';
import { updateGmailStatus, gmailStaleTooltip, updateSlackStatus, slackStaleTooltip } from './status-renderer';
import { I18N_DATA } from '../locales';

// Why: regression for the 2026-07 Gmail silent-failure incident — a connected card
// stayed green for 15 days of dead scans. The stale flag must surface on the dashboard.
describe('gmail scan staleness UI', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="gmailStatusLarge"></div>
            <div id="gmailStatusText"></div>
            <span id="gmailEmail"></span>
        `;
    });

    it('adds the stale modifier and tooltip when connected and stale', () => {
        updateGmailStatus(true, 'user@example.com', { stale: true, lastScanAt: 1753200000 });
        const card = document.getElementById('gmailStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(true);
        expect(card.title).toContain(I18N_DATA['en'].gmailScanStale);
    });

    it('clears the stale modifier when the scan is fresh again', () => {
        updateGmailStatus(true, 'user@example.com', { stale: true, lastScanAt: 1753200000 });
        updateGmailStatus(true, 'user@example.com', { stale: false, lastScanAt: 1753200000 });
        const card = document.getElementById('gmailStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
        expect(card.title).toBe('');
    });

    it('never marks a disconnected card stale', () => {
        updateGmailStatus(false, undefined, { stale: true });
        const card = document.getElementById('gmailStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
    });

    it('stays backward compatible when health is omitted', () => {
        updateGmailStatus(true, 'user@example.com');
        const card = document.getElementById('gmailStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
    });
});

describe('gmailStaleTooltip', () => {
    it('includes the last successful scan time when known', () => {
        const tooltip = gmailStaleTooltip(1753200000, 'en');
        expect(tooltip).toContain(I18N_DATA['en'].gmailLastScanAt);
        expect(tooltip).toContain(new Date(1753200000 * 1000).toLocaleString());
    });

    it('falls back to the stale label when the time is unknown', () => {
        expect(gmailStaleTooltip(undefined, 'en')).toBe(I18N_DATA['en'].gmailScanStale);
    });
});

// Why: regression for the 2026-09-17 Slack silent-failure incident — the bot was removed
// from every channel and the card stayed green for 10 days. The stale flag must surface.
describe('slack scan staleness UI', () => {
    beforeEach(() => {
        document.body.innerHTML = `
            <div id="slackStatusLarge"></div>
            <div id="slackStatusText"></div>
        `;
    });

    it('adds the stale modifier and tooltip when connected and stale', () => {
        updateSlackStatus(true, { stale: true, lastScanAt: 1758067200 });
        const card = document.getElementById('slackStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(true);
        expect(card.title).toContain(I18N_DATA['en'].slackScanStale);
    });

    it('clears the stale modifier when the scan is fresh again', () => {
        updateSlackStatus(true, { stale: true, lastScanAt: 1758067200 });
        updateSlackStatus(true, { stale: false, lastScanAt: 1758067200 });
        const card = document.getElementById('slackStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
        expect(card.title).toBe('');
    });

    it('never marks a disconnected card stale', () => {
        updateSlackStatus(false, { stale: true });
        const card = document.getElementById('slackStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
    });

    it('stays backward compatible when health is omitted', () => {
        updateSlackStatus(true);
        const card = document.getElementById('slackStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(false);
    });

    it('treats the "connected" status string the same as the boolean true', () => {
        updateSlackStatus('connected', { stale: true, lastScanAt: 1758067200 });
        const card = document.getElementById('slackStatusLarge')!;
        expect(card.classList.contains('c-status-card--stale')).toBe(true);
    });
});

describe('slackStaleTooltip', () => {
    it('includes the last successful scan time when known', () => {
        const tooltip = slackStaleTooltip(1758067200, 'en');
        expect(tooltip).toContain(I18N_DATA['en'].slackLastScanAt);
        expect(tooltip).toContain(new Date(1758067200 * 1000).toLocaleString());
    });

    it('falls back to the stale label when the time is unknown', () => {
        expect(slackStaleTooltip(undefined, 'en')).toBe(I18N_DATA['en'].slackScanStale);
    });
});
