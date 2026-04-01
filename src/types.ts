import { z } from 'zod';

/**
 * Task Represents a work item from the 'messages' table.
 * Fields according to the DB schema and specific user requirements.
 * Note: priority and assignee are intentionally excluded in this version.
 */
export interface Task {
    id: number;
    room: string;
    task: string; // The content of the task
    category: string; // Maps to 'status' (e.g., 'todo', 'waiting')
    requester?: string;
    assignee?: string;
    originalText?: string;
    createdAt?: string;
}

/**
 * IncomingMessage represents a message received from WhatsApp.
 */
export interface IncomingMessage {
    roomId: string;
    sender: string;
    text: string;
    timestamp: string;
}

/**
 * Zod schema to validate LLM response for the new Master Prompt.
 */
export const MasterActionSchema = z.object({
    action: z.enum(['new', 'update', 'resolve']),
    target_task_id: z.number().nullable(),
    task: z.string(),
    requester: z.string(),
    assignee: z.string(),
    status: z.enum(['todo', 'waiting', 'completed'])
});

export const MasterResponseSchema = z.array(MasterActionSchema);

export type MasterAction = z.infer<typeof MasterActionSchema>;
export type MasterResponse = z.infer<typeof MasterResponseSchema>;
