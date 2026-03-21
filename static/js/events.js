/**
 * Simple Event Emitter for decoupled frontend communication.
 */
class EventEmitter {
    constructor() {
        this.events = {};
    }

    on(event, listener) {
        if (!this.events[event]) {
            this.events[event] = [];
        }
        this.events[event].push(listener);
    }

    off(event, listener) {
        if (!this.events[event]) return;
        this.events[event] = this.events[event].filter(l => l !== listener);
    }

    emit(event, data) {
        if (!this.events[event]) return;
        this.events[event].forEach(listener => listener(data));
    }
}

export const events = new EventEmitter();

// Defined Event Constants
export const EVENTS = {
    TASK_COMPLETED: 'task:completed',
    TASK_DELETED: 'task:deleted',
    USER_PROFILE_UPDATED: 'user:profile_updated',
    THEME_CHANGED: 'theme:changed',
    LANGUAGE_CHANGED: 'language:changed'
};
