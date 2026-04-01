import { Task } from '../types.ts';
import { fetchWithRetry } from '../utils/fetch-utils.ts';

/**
 * Interface definition for Task Repository to ensure loose coupling.
 */
export interface ITaskRepository {
    /**
     * Retrieves active tasks (status=todo or waiting) for a specific room.
     * Excludes sender/receiver filters, focus only on roomId.
     */
    getActiveTasks(roomId: string): Promise<Task[]>;

    /**
     * Creates a new task.
     */
    createTask(roomId: string, task: string, requester: string, assignee: string, status: string): Promise<void>;

    /**
     * Updates an existing task's state or content.
     */
    updateTask(taskId: number, task: string, requester: string, assignee: string, status: string): Promise<void>;
}

/**
 * TaskRepository implementation using the Fetch API.
 */
export class FetchTaskRepository implements ITaskRepository {
    /**
     * Fetches active tasks for a given room ID.
     * Maps to Go backend GET /api/messages.
     */
    async getActiveTasks(roomId: string): Promise<Task[]> {
        // Fetch tasks for the room. The backend should handle status filtering if possible, 
        // otherwise we filter here. The requirement specifies status: 'todo' | 'waiting'.
        const url = `/api/messages?room=${encodeURIComponent(roomId)}`;
        const response = await fetchWithRetry(url, { method: 'GET' });

        if (!response.ok) {
            throw new Error(`Failed to fetch active tasks: ${response.statusText}`);
        }

        const data = await response.json();
        return (data || [])
            .filter((msg: any) => msg.category === 'todo' || msg.category === 'waiting')
            .map((msg: any) => ({
                id: msg.id,
                room: msg.room,
                task: msg.task,
                category: msg.category,
                originalText: msg.original_text,
                requester: msg.requester,
                assignee: msg.assignee
            }));
    }

    async createTask(roomId: string, task: string, requester: string, assignee: string, status: string): Promise<void> {
        const url = '/api/messages/create'; // Assuming this endpoint exists
        const body = {
            room: roomId,
            task: task,
            requester: requester,
            assignee: assignee,
            category: status
        };

        const response = await fetchWithRetry(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });

        if (!response.ok) {
            throw new Error(`Failed to create task: ${response.statusText}`);
        }
    }

    async updateTask(taskId: number, task: string, requester: string, assignee: string, status: string): Promise<void> {
        const url = '/api/messages/update';
        const body = {
            id: taskId,
            task: task,
            requester: requester,
            assignee: assignee,
            category: status
        };

        const response = await fetchWithRetry(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });

        if (!response.ok) {
            throw new Error(`Failed to update task: ${response.statusText}`);
        }
    }
}
