/**
 * @file task-context.ts
 * @description Logic for parsing and handling AI-extracted task context justifications.
 */

/**
 * Safely parses the consolidated context from the backend.
 * Why: The backend stores this as a JSON array string or a slice of strings depending on the API stage.
 */
export function parseTaskContext(context: any): string[] {
    if (!context) return [];
    
    if (Array.isArray(context)) {
        return context;
    }
    
    if (typeof context === 'string') {
        try {
            // Check if it's already a JSON string representation of an array
            const parsed = JSON.parse(context);
            if (Array.isArray(parsed)) return parsed;
        } catch (e) {
            // Not JSON, treat as a single string if not empty
            if (context.trim()) return [context.trim()];
        }
    }
    
    return [];
}
