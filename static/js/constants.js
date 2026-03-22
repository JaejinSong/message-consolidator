/**
 * @file constants.js
 * @description Centralized constants for DOM IDs and service statuses to prevent sync issues.
 */

export const SERVICE_IDS = {
    SLACK: 'slack',
    WHATSAPP: 'wa',
    GMAIL: 'gmail'
};

export const DOM_IDS = {
    STATUS_LARGE: (service) => `${service}StatusLarge`, // e.g., slackStatusLarge
    STATUS_TEXT: (service) => `${service}StatusText`,   // e.g., slackStatusText
    WHATSAPP_DOT: 'waStatusLarge', // Keep consistent with waStatusLarge
    WHATSAPP_TEXT: 'waStatusText'
};

export const STATUS_STATES = {
    CONNECTED: 'CONNECTED',
    AUTHENTICATED: 'authenticated',
    OFFLINE: 'OFFLINE',
    DISCONNECTED: 'DISCONNECTED'
};

export const UI_TEXT = {
    CONNECTED: '연결됨',
    DISCONNECTED: '연결 안됨',
    ON: 'ON',
    OFF: 'OFF'
};
