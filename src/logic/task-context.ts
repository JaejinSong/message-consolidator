/**
 * @file task-context.ts
 * @description Logic for parsing and handling AI-extracted task context justifications.
 */

/**
 * Safely parses the consolidated context from the backend.
 * Why: The backend stores this as a JSON array string or a slice of strings depending on the API stage.
 */
export function parseTaskContext(context: unknown): string[] {
    if (!context) return [];

    if (Array.isArray(context)) {
        return context.filter((v): v is string => typeof v === 'string');
    }

    if (typeof context === 'string') {
        try {
            const parsed: unknown = JSON.parse(context);
            if (Array.isArray(parsed)) {
                return parsed.filter((v): v is string => typeof v === 'string');
            }
        } catch {
            if (context.trim()) return [context.trim()];
        }
    }

    return [];
}
