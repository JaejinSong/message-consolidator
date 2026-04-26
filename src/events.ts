/**
 * @file events.ts
 * @description Type-safe Event Emitter for decoupled frontend communication.
 */

// Why: generic event bus carries heterogeneous payloads. The runtime store keeps
// listeners as `EventCallback<unknown>` while `on/emit` accept a per-call generic
// so callers keep typed callbacks without `any`.
export type EventCallback<T = unknown> = (data: T) => void;

class EventEmitter {
    private events: Record<string, EventCallback<unknown>[]> = {};

    on<T = unknown>(event: string, listener: EventCallback<T>): void {
        if (!this.events[event]) {
            this.events[event] = [];
        }
        this.events[event].push(listener as EventCallback<unknown>);
    }

    off<T = unknown>(event: string, listener: EventCallback<T>): void {
        if (!this.events[event]) return;
        this.events[event] = this.events[event].filter(l => l !== (listener as EventCallback<unknown>));
    }

    emit<T = unknown>(event: string, data: T): void {
        if (!this.events[event]) return;
        this.events[event].forEach(listener => listener(data));
    }
}

export const events = new EventEmitter();

// Defined Event Constants
export const EVENTS = {
    TASK_COMPLETED: 'task:completed',
    USER_PROFILE_UPDATED: 'user:profile_updated',
    THEME_CHANGED: 'theme:changed',
    LANGUAGE_CHANGED: 'language:changed'
} as const;
