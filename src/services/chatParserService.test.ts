import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ChatParserService, ILLMProvider } from './chatParserService';
import { ITaskRepository } from '../repositories/TaskRepository.ts';
import { IncomingMessage, Task } from '../types.ts';

/**
 * Mock implementation of ITaskRepository
 */
class MockTaskRepository implements ITaskRepository {
    getActiveTasks = vi.fn();
    createTask = vi.fn();
    updateTask = vi.fn();
}

/**
 * Mock implementation of ILLMProvider
 */
class MockLLMProvider implements ILLMProvider {
    processChat = vi.fn();
}

describe('ChatParserService', () => {
    let repo: MockTaskRepository;
    let llm: MockLLMProvider;
    let service: ChatParserService;

    const mockMessage: IncomingMessage = {
        roomId: 'room1',
        sender: 'user1',
        text: 'Task is done now.',
        timestamp: '2023-10-01T12:00:00Z'
    };

    beforeEach(() => {
        repo = new MockTaskRepository();
        llm = new MockLLMProvider();
        service = new ChatParserService(repo as any, llm as any);
        vi.clearAllMocks();
    });

    it('should route to Case A if active tasks exist', async () => {
        const mockTasks: Task[] = [{ id: 1, room: 'room1', task: 'Sample', category: 'todo' }];
        repo.getActiveTasks.mockResolvedValue(mockTasks);
        llm.processChat.mockResolvedValue([
            {
                action: 'resolve',
                target_task_id: 1,
                task: 'Sample',
                requester: 'user1',
                assignee: 'User',
                status: 'completed'
            }
        ]);

        await service.onMessagesReceived('room1', [mockMessage]);

        expect(repo.getActiveTasks).toHaveBeenCalledWith('room1');
        expect(llm.processChat).toHaveBeenCalledWith('User', mockTasks, [mockMessage]);
        expect(repo.updateTask).toHaveBeenCalledWith(1, 'Sample', 'user1', 'User', 'completed');
    });

    it('should route to Case B (new task) if no active tasks exist', async () => {
        repo.getActiveTasks.mockResolvedValue([]);
        llm.processChat.mockResolvedValue([
            {
                action: 'new',
                target_task_id: null,
                task: 'Buy milk',
                requester: 'user1',
                assignee: 'User',
                status: 'todo'
            }
        ]);

        await service.onMessagesReceived('room1', [mockMessage]);

        expect(repo.getActiveTasks).toHaveBeenCalledWith('room1');
        expect(repo.createTask).toHaveBeenCalledWith('room1', 'Buy milk', 'user1', 'User', 'todo');
    });

    it('should handle validation failure and return error', async () => {
        repo.getActiveTasks.mockResolvedValue([{ id: 1, room: 'room1', task: 'Sample', category: 'todo' }]);
        // Invalid response (not an array)
        llm.processChat.mockResolvedValue({ action: 'resolve' });

        const result = await service.onMessagesReceived('room1', [mockMessage]);

        expect(result.success).toBe(false);
        expect(result.error).toBe('Invalid LLM response');
        expect(repo.updateTask).not.toHaveBeenCalled();
    });

    it('should handle "update" action correctly', async () => {
        repo.getActiveTasks.mockResolvedValue([{ id: 1, room: 'room1', task: 'Follow up', category: 'todo' }]);
        llm.processChat.mockResolvedValue([
            {
                action: 'update',
                target_task_id: 1,
                task: 'New info added',
                requester: 'user1',
                assignee: 'User',
                status: 'waiting'
            }
        ]);

        await service.onMessagesReceived('room1', [mockMessage]);

        expect(repo.updateTask).toHaveBeenCalledWith(1, 'New info added', 'user1', 'User', 'waiting');
    });

    it('should process multiple actions from a single chat session', async () => {
        repo.getActiveTasks.mockResolvedValue([{ id: 2, room: 'room1', task: 'Old Task', category: 'todo' }]);
        llm.processChat.mockResolvedValue([
            {
                action: 'update',
                target_task_id: 2,
                task: 'Updated Task',
                requester: 'user1',
                assignee: 'User',
                status: 'todo'
            },
            {
                action: 'new',
                target_task_id: null,
                task: 'Another Task',
                requester: 'user1',
                assignee: 'User',
                status: 'todo'
            }
        ]);

        await service.onMessagesReceived('room1', [mockMessage]);

        expect(repo.updateTask).toHaveBeenCalledTimes(1);
        expect(repo.createTask).toHaveBeenCalledTimes(1);
    });

    it('should process messages sequentially for the same roomID', async () => {
        // This test simulates race condition by having the first task delay its completion
        repo.getActiveTasks.mockImplementation(async () => {
            await new Promise(resolve => setTimeout(resolve, 50));
            return [];
        });
        
        llm.processChat.mockResolvedValue([]);

        const p1 = service.onMessagesReceived('room1', [mockMessage]);
        const p2 = service.onMessagesReceived('room1', [mockMessage]);

        // p1 and p2 should result in sequential execution
        await Promise.all([p1, p2]);
        
        expect(repo.getActiveTasks).toHaveBeenCalledTimes(2);
    });
});
