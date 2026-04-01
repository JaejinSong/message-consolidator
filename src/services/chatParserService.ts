import { Task, IncomingMessage, MasterResponseSchema, MasterAction } from '../types.ts';
import { ITaskRepository } from '../repositories/TaskRepository.ts';

/**
 * Interface for the LLM interaction layer.
 * This can be implemented with actual API calls or mocked for testing.
 */
export interface ILLMProvider {
    processChat(targetUser: string, activeTasks: Task[], newMessages: IncomingMessage[]): Promise<any>;
}

/**
 * ChatParserService handles the logic of mapping incoming WhatsApp messages
 * to existing tasks or creating new ones while ensuring thread safety per room.
 */
export class ChatParserService {
    private repo: ITaskRepository;
    private llm: ILLMProvider;
    private roomQueues: Map<string, Promise<any>> = new Map();
    private targetUser: string;

    /**
     * @param {ITaskRepository} repo - Repository for Database operations.
     * @param {ILLMProvider} llm - Provider for LLM evaluations.
     * @param {string} targetUser - The owner/main user of the service.
     */
    constructor(repo: ITaskRepository, llm: ILLMProvider, targetUser: string = "User") {
        this.repo = repo;
        this.llm = llm;
        this.targetUser = targetUser;
    }

    /**
     * Main entry point for processing an incoming message.
     * In the new logic, we might process a batch or single message.
     * If called every 1 minute, we handle the chunk.
     */
    public async onMessagesReceived(roomId: string, messages: IncomingMessage[]): Promise<any> {
        const existingQueue = this.roomQueues.get(roomId) || Promise.resolve();
        
        const currentTask = existingQueue.then(() => this.processMessageLogic(roomId, messages))
            .catch(err => {
                console.error(`[ChatParserService] Critical error in room ${roomId}:`, err);
                return { success: false, error: err.message };
            });

        this.roomQueues.set(roomId, currentTask);
        return currentTask;
    }

    /**
     * Core business logic for message parsing and task mapping.
     */
    private async processMessageLogic(roomId: string, messages: IncomingMessage[]): Promise<any> {
        // 1. Fetch active tasks for the room (Only by roomId)
        const activeTasks = await this.repo.getActiveTasks(roomId);

        // 2. Call LLM with the Master Prompt
        const rawResponse = await this.llm.processChat(this.targetUser, activeTasks, messages);

        // 3. Validate response
        const parsed = MasterResponseSchema.safeParse(rawResponse);
        if (!parsed.success) {
            console.error('[ChatParserService] LLM response validation failed:', parsed.error);
            return { success: false, error: 'Invalid LLM response' };
        }

        const actions = parsed.data;

        // 4. Execute actions
        for (const action of actions) {
            await this.executeAction(roomId, action);
        }

        return { success: true, actionCount: actions.length };
    }

    /**
     * Handles individual action execution.
     */
    private async executeAction(roomId: string, action: MasterAction): Promise<void> {
        try {
            switch (action.action) {
                case 'new':
                    await this.repo.createTask(
                        roomId,
                        action.task,
                        action.requester,
                        action.assignee,
                        action.status
                    );
                    break;
                case 'update':
                case 'resolve':
                    if (action.target_task_id) {
                        // 'resolve' maps to status 'completed' or as returned by LLM
                        await this.repo.updateTask(
                            action.target_task_id,
                            action.task,
                            action.requester,
                            action.assignee,
                            action.status
                        );
                    } else {
                        console.warn('[ChatParserService] Update/Resolve requested without target_task_id');
                    }
                    break;
            }
        } catch (err) {
            console.error('[ChatParserService] Error executing action:', action, err);
        }
    }
}
