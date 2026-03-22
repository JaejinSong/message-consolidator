/**
 * @file verify_renderer.js
 * @description Script to verify renderer-related logic without a browser.
 */

import { I18N_DATA } from './locales.js';

function testEmptyStateMessages() {
    console.log('--- Testing Empty State Messages ---');
    const lang = 'ko';
    const messages = I18N_DATA[lang].emptyStateMessages;

    console.assert(messages && messages.length >= 15, 'Should have at least 15 witty messages for Korean');

    // Check for specific natural phrasing improvements
    const hasCoffee = messages.some(m => m.includes('커피'));
    const hasPowerHouse = messages.some(m => m.includes('화력 발전소'));

    console.assert(hasCoffee, 'Witty message should contain "커피"');
    console.assert(hasPowerHouse, 'Witty message should contain "화력 발전소"');

    console.log('✅ Empty State Messages verified');
}

testEmptyStateMessages();
