import { Task, IncomingMessage } from '../types.ts';
import { ILLMProvider } from './chatParserService.ts';

export class OpenAIProvider implements ILLMProvider {
    private apiKey: string;

    constructor(apiKey: string) {
        this.apiKey = apiKey;
    }

    async processChat(targetUser: string, activeTasks: Task[], newMessages: IncomingMessage[]): Promise<any> {
        const prompt = this.buildMasterPrompt(targetUser, activeTasks, newMessages);
        
        // This is a placeholder for the actual LLM API call (e.g., OpenAI, Claude, etc.)
        // In a real implementation, you would use fetch or an SDK here.
        console.log("--- Master Prompt ---");
        console.log(prompt);
        
        // Mocking LLM response for demonstration
        // In reality, this would be: 
        // const response = await fetch('...', { body: JSON.stringify({ prompt, ... }) });
        // return await response.json();
        return []; 
    }

    private buildMasterPrompt(targetUser: string, activeTasks: Task[], newMessages: IncomingMessage[]): string {
        const tasksJson = JSON.stringify(activeTasks.map(t => ({
            id: t.id,
            task: t.task,
            requester: (t as any).requester,
            assignee: (t as any).assignee,
            status: t.category
        })), null, 2);

        const messagesJson = JSON.stringify(newMessages.map(m => ({
            sender: m.sender,
            text: m.text,
            time: m.timestamp
        })), null, 2);

        return `Target User: ${targetUser}
[INPUT DATA]
- Active Tasks: ${tasksJson}
- New Messages: ${messagesJson}

[ANALYSIS & RECONCILIATION RULES]
1. ENTITY NORMALIZATION (CRITICAL): 
- Remove Indonesian honorifics such as "mas", "pak", "mbak", "bapak", "ibu". Extract ONLY the actual names.
- Resolve pronouns like "나", "me", "I" to the actual sender's name based on the message context. DO NOT use phone numbers (e.g., "@6281...") if a name is inferable.
2. UPDATE vs. NEW:
- Compare 'New Messages' against 'Active Tasks'.
- If the new message is an answer, progress, or follow-up to an Active Task, you MUST output "action": "update" or "resolve".
- Output "action": "new" ONLY if the message discusses a completely independent topic not present in Active Tasks.
3. 1 TOPIC = 1 TASK: Synthesize the core intent. Do not quote verbatim.

[OUTPUT FORMAT]
Return a JSON array of actions. Use "" for missing fields.
[
  {
    "action": "new|update|resolve",
    "target_task_id": "ID of the Active Task (Use null if action is 'new')",
    "task": "High-level summary of the pending action or conclusion",
    "requester": "Normalized Name",
    "assignee": "Normalized Name",
    "status": "todo|waiting|completed"
  }
]
Output ONLY valid JSON array.`;
    }
}
