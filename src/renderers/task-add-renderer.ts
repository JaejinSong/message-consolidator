/**
 * @file task-add-renderer.ts
 * @description Manual "add missed task" panel -- lets the user record a task the AI
 * missed (the highest-value correction-learning signal, see services.RecordManualAdd).
 */
import { api } from '../api';
import { state } from '../state';
import { events, EVENTS } from '../events';
import { getCategoryOptionsHtml } from '../logic';
import { getErrorMessage } from '../utils';
import { showToast } from './ui-effects';

let bound = false;

/**
 * Wires the toggle button, form submit/cancel and Escape-to-close. Safe to call once;
 * `onCreated` is invoked after a successful create so the caller can refresh the grid.
 */
export function initTaskAddPanel(onCreated: () => void | Promise<void>): void {
    if (bound) return;
    bound = true;

    populateCategoryOptions();
    events.on(EVENTS.LANGUAGE_CHANGED, () => populateCategoryOptions());

    document.getElementById('taskAddToggleBtn')?.addEventListener('click', () => togglePanel());
    document.getElementById('taskAddCancelBtn')?.addEventListener('click', () => closePanel());
    document.getElementById('taskAddSubmitBtn')?.addEventListener('click', () => void submit(onCreated));

    document.getElementById('taskAddPanel')?.addEventListener('keydown', (e) => {
        if ((e as KeyboardEvent).key === 'Escape') closePanel();
    });
}

function populateCategoryOptions(): void {
    const select = document.getElementById('taskAddCategory') as HTMLSelectElement | null;
    if (!select) return;
    const lang = state.currentLang || 'en';
    const selected = select.value || 'TASK';
    select.innerHTML = getCategoryOptionsHtml(selected, lang);
}

function togglePanel(): void {
    const panel = document.getElementById('taskAddPanel');
    if (!panel) return;
    const opening = panel.classList.contains('hidden');
    panel.classList.toggle('hidden', !opening);
    if (opening) {
        (document.getElementById('taskAddTask') as HTMLInputElement | null)?.focus();
    }
}

function closePanel(): void {
    document.getElementById('taskAddPanel')?.classList.add('hidden');
    resetForm();
}

function resetForm(): void {
    ['taskAddTask', 'taskAddAssignee', 'taskAddDeadline', 'taskAddRoom', 'taskAddOriginal'].forEach(id => {
        const el = document.getElementById(id) as HTMLInputElement | HTMLTextAreaElement | null;
        if (el) el.value = '';
    });
    populateCategoryOptions();
}

async function submit(onCreated: () => void | Promise<void>): Promise<void> {
    const lang = state.currentLang || 'en';
    const task = (document.getElementById('taskAddTask') as HTMLInputElement | null)?.value.trim() || '';
    if (!task) {
        showToast(lang === 'ko' ? '업무 제목을 입력해 주세요' : 'Task title is required', 'error');
        return;
    }

    const assignee = (document.getElementById('taskAddAssignee') as HTMLInputElement | null)?.value.trim() || '';
    const deadline = (document.getElementById('taskAddDeadline') as HTMLInputElement | null)?.value.trim() || '';
    const room = (document.getElementById('taskAddRoom') as HTMLInputElement | null)?.value.trim() || '';
    const category = (document.getElementById('taskAddCategory') as HTMLSelectElement | null)?.value || 'TASK';
    const originalText = (document.getElementById('taskAddOriginal') as HTMLTextAreaElement | null)?.value.trim() || '';

    try {
        await api.createMessage({
            task,
            assignee: assignee || undefined,
            deadline: deadline || undefined,
            category,
            room: room || undefined,
            original_text: originalText || undefined
        });
        closePanel();
        showToast(lang === 'ko' ? '업무가 추가되었습니다' : 'Task added', 'success');
        await onCreated();
    } catch (err: unknown) {
        showToast(getErrorMessage(err) || (lang === 'ko' ? '업무 추가 실패' : 'Failed to add task'), 'error');
    }
}
