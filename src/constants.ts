/**
 * @file constants.ts
 * @description Centralized constants for DOM IDs and service statuses with TypeScript.
 */

export const SERVICE_IDS = {
    SLACK: 'slack',
    WHATSAPP: 'wa',
    GMAIL: 'gmail'
} as const;

export type ServiceId = typeof SERVICE_IDS[keyof typeof SERVICE_IDS];

export const DOM_IDS = {
    STATUS_LARGE: (service: string) => `${service}StatusLarge`,
    STATUS_TEXT: (service: string) => `${service}StatusText`,
    WHATSAPP_DOT: 'waStatusLarge',
    WHATSAPP_TEXT: 'waStatusText'
} as const;

export const STATUS_STATES = {
    CONNECTED: 'CONNECTED',
    AUTHENTICATED: 'authenticated',
    OFFLINE: 'OFFLINE',
    DISCONNECTED: 'DISCONNECTED'
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
    TOKEN_USAGE: 60000
} as const;
