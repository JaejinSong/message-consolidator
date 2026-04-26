/**
 * @file constants.ts
 * @description Centralized constants for DOM IDs and service statuses with TypeScript.
 */

export const SERVICE_IDS = {
    SLACK: 'slack',
    WHATSAPP: 'wa',
    GMAIL: 'gmail',
    TELEGRAM: 'telegram'
} as const;

export type ServiceId = typeof SERVICE_IDS[keyof typeof SERVICE_IDS];

export const ASSIGNEE_SHARED = 'shared';

export const DOM_IDS = {
    STATUS_LARGE: (service: string) => `${service}StatusLarge`,
    STATUS_TEXT: (service: string) => `${service}StatusText`,
    WHATSAPP_DOT: 'waStatusLarge',
    WHATSAPP_TEXT: 'waStatusText',
    TELEGRAM_STATUS_LARGE: 'telegramStatusLarge',
    TELEGRAM_STATUS_TEXT: 'telegramStatusText'
} as const;

/**
 * Channel status string standard.
 *
 * Convention: backend status responses are lowercase (e.g. "connected", "disconnected").
 * Per-channel handlers in /handlers/handlers_*.go must follow the same convention.
 * Frontend code should compare via `isStatusConnected` (src/utils.ts) rather than direct
 * string equality, so a casing slip on either side is detected by the case-insensitive guard.
 */
export const STATUS_STATES = {
    CONNECTED: 'connected',
    AUTHENTICATED: 'authenticated',
    OFFLINE: 'offline',
    DISCONNECTED: 'disconnected'
} as const;

export const UI_TEXT = {
    CONNECTED: '연결됨',
    DISCONNECTED: '연결 안됨',
    ON: 'ON',
    OFF: 'OFF'
} as const;

export const POLLING_INTERVALS = {
    MESSAGES: 60000,
    WHATSAPP: 10000,
    SLACK: 10000,
    GMAIL: 30000,
    TELEGRAM: 10000,
    TOKEN_USAGE: 60000
} as const;

export const TELEGRAM_STATUS = {
    CONNECTED: 'connected',
    PENDING_CODE: 'pending_code',
    PENDING_PASSWORD: 'pending_password',
    DISCONNECTED: 'disconnected'
} as const;

export type TelegramStatus = typeof TELEGRAM_STATUS[keyof typeof TELEGRAM_STATUS];
