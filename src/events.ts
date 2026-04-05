/**
 * @file events.ts
 * @description Type-safe Event Emitter for decoupled frontend communication.
 */

export type EventCallback = (data?: any) => void;

class EventEmitter {
    private events: Record<string, EventCallback[]> = {};

    /**
     * Subscribes to an event.
     */
    on(event: string, listener: EventCallback): void {
        if (!this.events[event]) {
            this.events[event] = [];
        }
        this.events[event].push(listener);
    }

    /**
     * Unsubscribes from an event.
     */
    off(event: string, listener: EventCallback): void {
        if (!this.events[event]) return;
        this.events[event] = this.events[event].filter(l => l !== listener);
    }

    /**
     * Emits an event with optional data.
     */
    emit(event: string, data?: any): void {
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
